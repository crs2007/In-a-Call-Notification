// Package engine runs the detection loop and decides what reaches the broker.
//
// It answers three separate questions in order, and keeping them separate is
// what stops this from becoming a pile of app-specific special cases:
//
//	Can I detect a call?      -> detectors and the resolver
//	Am I allowed to say so?   -> the network matcher
//	Has anything changed?     -> the debouncing state machine
package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/detection"
	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/mqtt"
	"github.com/crs2007/callmqtt/internal/network"
)

// Publisher is the engine's view of the broker. Declaring it here rather than
// depending on the concrete client keeps the whole gating matrix testable
// without a broker.
type Publisher interface {
	PublishState(ctx context.Context, p mqtt.Payload) error
}

// Status is a snapshot for the tray and for `callmqtt diagnose`.
type Status struct {
	State       model.CallState
	App         string
	Apps        []string
	Confidence  float64
	Reasons     []string
	Network     network.Info
	NetworkRule string
	Allowed     bool
	LastChange  time.Time
}

// Engine ties detection, network policy and publishing together.
type Engine struct {
	cfg       *config.Config
	log       *slog.Logger
	detectors []model.Detector
	checker   network.Checker
	matcher   *network.Matcher
	machine   *detection.Machine
	publisher Publisher

	// Network state is refreshed on its own slower cadence; interfaces change
	// far less often than call state.
	netInfo    network.Info
	netRule    string
	netAllowed bool
	netCheckAt time.Time
	netKnown   bool

	wasAllowed  bool
	lastPublish time.Time
	published   bool
	status      Status
}

// Options collects the engine's dependencies. Everything platform-specific
// arrives through here, injected by main.
type Options struct {
	Config    *config.Config
	Logger    *slog.Logger
	Detectors []model.Detector
	Checker   network.Checker
	Publisher Publisher
}

// New builds an engine. The network allow-list is compiled here so a malformed
// rule fails at startup rather than at the first poll.
func New(opts Options) (*Engine, error) {
	matcher, err := network.NewMatcher(opts.Config.AllowedNetworks)
	if err != nil {
		return nil, err
	}

	return &Engine{
		cfg:       opts.Config,
		log:       opts.Logger,
		detectors: opts.Detectors,
		checker:   opts.Checker,
		matcher:   matcher,
		machine:   detection.NewMachine(opts.Config.Detection),
		publisher: opts.Publisher,
		status:    Status{State: model.StateUnknown},
	}, nil
}

// Run polls until the context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.cfg.Poll.Detect())
	defer ticker.Stop()

	e.evaluate(ctx, time.Now())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			e.evaluate(ctx, time.Now())
		}
	}
}

// Status returns the most recent snapshot.
func (e *Engine) Status() Status { return e.status }

// Evaluate runs one poll cycle at the given time. Run supplies the real clock;
// tests supply their own.
func (e *Engine) Evaluate(ctx context.Context, now time.Time) { e.evaluate(ctx, now) }

func (e *Engine) evaluate(ctx context.Context, now time.Time) {
	results := make([]model.DetectionResult, 0, len(e.detectors))
	for _, d := range e.detectors {
		results = append(results, d.Detect(ctx))
	}
	resolved := detection.Resolve(results)

	e.refreshNetwork(ctx, now)

	state, changed := e.machine.Update(resolved, now)

	e.status = Status{
		State:       state,
		App:         resolved.App,
		Apps:        resolved.Apps,
		Confidence:  resolved.Confidence,
		Reasons:     resolved.Reasons,
		Network:     e.netInfo,
		NetworkRule: e.netRule,
		Allowed:     e.netAllowed,
		LastChange:  e.status.LastChange,
	}
	if changed {
		e.status.LastChange = now
		e.log.Info("call state changed",
			"state", state, "app", resolved.App, "confidence", resolved.Confidence,
			"network", e.netRule, "publishing", e.netAllowed)
	}

	// Publishing is gated on the network, and nothing about that gate is
	// negotiable: off an allowed network the agent goes silent, and Home
	// Assistant's expire_after resolves the entity on its own.
	if !e.netAllowed {
		if e.wasAllowed {
			e.log.Info("left allowed network, suspending publishing", "state", state)
		}
		e.wasAllowed = false
		return
	}

	// Rejoining an allowed network republishes immediately, so Home Assistant
	// is never left waiting a whole heartbeat to catch up.
	rejoined := !e.wasAllowed
	e.wasAllowed = true
	if rejoined {
		e.log.Info("joined allowed network, resuming publishing", "network", e.netRule)
	}

	heartbeatDue := e.published && now.Sub(e.lastPublish) >= e.cfg.Poll.Heartbeat()

	// The first publish always happens, even though the state is still
	// unknown. The state topic is retained, so after a restart the broker is
	// still holding whatever was published before — possibly "active" from a
	// call that ended while the agent was down. Announcing "unknown" at once
	// clears that, instead of leaving the light red for a whole exit debounce
	// every time the agent restarts.
	startupAnnouncement := !e.published

	if !changed && !rejoined && !heartbeatDue && !startupAnnouncement {
		return
	}

	payload := mqtt.Payload{
		Device:     e.cfg.App.DeviceID,
		State:      string(state),
		Confidence: resolved.Confidence,
		Network:    e.netRule,
		Timestamp:  now,
	}
	// The app is only meaningful while a call is in progress. Naming the last
	// app seen on an inactive payload would be misleading.
	if state == model.StateActive {
		payload.App = resolved.App
		payload.Apps = resolved.Apps
	}

	if err := e.publisher.PublishState(ctx, payload); err != nil {
		e.log.Error("publish state", "error", err, "state", state)
		return
	}
	e.published = true
	e.lastPublish = now
}

// refreshNetwork re-reads the network on its own slower interval.
//
// A failed read denies. Not knowing where the machine is connected is not a
// reason to assume it is somewhere safe.
func (e *Engine) refreshNetwork(ctx context.Context, now time.Time) {
	if e.netKnown && now.Sub(e.netCheckAt) < e.cfg.Poll.Network() {
		return
	}
	e.netCheckAt = now
	e.netKnown = true

	info, err := e.checker.Current(ctx)
	if err != nil {
		e.log.Warn("network check failed, suspending publishing", "error", err)
		e.netInfo, e.netRule, e.netAllowed = network.Info{}, "", false
		return
	}

	rule, allowed := e.matcher.Match(info)
	if allowed != e.netAllowed || rule != e.netRule {
		e.log.Info("network changed",
			"interface", info.Interface, "rule", rule, "allowed", allowed)
	}
	e.netInfo, e.netRule, e.netAllowed = info, rule, allowed
}
