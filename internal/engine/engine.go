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
	"sync"
	"sync/atomic"
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
	Paused      bool

	// EvaluatedAt is when this snapshot was computed, regardless of whether
	// it was ever actually published (a poll tick with nothing new to say
	// still updates it). CurrentPayload uses it as a republished Payload's
	// Timestamp on a broker reconnect — see CurrentPayload for why replaying
	// it rather than stamping the reconnect moment is the honest choice.
	EvaluatedAt time.Time
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

	wasAllowed     bool
	lastPublish    time.Time
	published      bool
	pending        bool
	publishFailing bool
	statusMu       sync.RWMutex
	status         Status
	// evaluated is guarded by statusMu, alongside status: it flips true the
	// first time evaluate() runs and never back, marking the point from
	// which status.EvaluatedAt (and therefore CurrentPayload) is trustworthy.
	evaluated bool
	paused    atomic.Bool
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
// rule fails at startup rather than at the first poll. It is also checked
// here against what opts.Checker can actually observe, so a rule that can
// never match on this platform (SSID-only with no WLAN support, say) fails
// startup too rather than leaving the bulb permanently dark with no
// diagnostic.
func New(opts Options) (*Engine, error) {
	matcher, err := network.NewMatcher(opts.Config.AllowedNetworks)
	if err != nil {
		return nil, err
	}
	if err := network.CheckCapabilities(opts.Config.AllowedNetworks, opts.Checker.Capabilities()); err != nil {
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

// Status returns the most recent snapshot. Safe to call from any goroutine —
// the tray calls it from one that isn't running evaluate.
func (e *Engine) Status() Status {
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()
	return copyStatus(e.status)
}

// CurrentPayload returns the mqtt.Payload for the most recently evaluated
// status, or ok == false if evaluate has never run (e.g. at startup, before
// Run's first poll). It is meant to be wired into mqtt.Client.SetStateSource
// — see Publisher and the wiring in internal/supervisor — so a broker
// reconnect republishes whatever the engine currently thinks the state is,
// instead of only "online" and the discovery config.
func (e *Engine) CurrentPayload() (mqtt.Payload, bool) {
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()
	if !e.evaluated {
		return mqtt.Payload{}, false
	}
	return payloadFromStatus(e.cfg.App.DeviceID, e.status), true
}

// payloadFromStatus builds the wire payload for one status snapshot. It is
// the one place a Status becomes an mqtt.Payload, used both by evaluate's
// normal publish path and by CurrentPayload's reconnect republish, so the two
// can never drift on which fields travel and which don't.
//
// Timestamp is deliberately s.EvaluatedAt, not time.Now(): when this is
// called from CurrentPayload on a reconnect, replaying the time the state was
// actually last evaluated is the honest value — the state itself hasn't
// changed, only the fact that the broker forgot it. Stamping the reconnect
// moment instead would claim a fresher observation than was actually made.
func payloadFromStatus(deviceID string, s Status) mqtt.Payload {
	p := mqtt.Payload{
		Device:     deviceID,
		State:      string(s.State),
		Confidence: s.Confidence,
		Network:    s.NetworkRule,
		Timestamp:  s.EvaluatedAt,
	}
	// The app is only meaningful while a call is in progress. Naming the last
	// app seen on an inactive payload would be misleading.
	if s.State == model.StateActive {
		p.App = s.App
		p.Apps = s.Apps
	}
	return p
}

// copyStatus deep-copies the slice fields so a caller can't alias the
// engine's internal buffers.
func copyStatus(s Status) Status {
	if s.Apps != nil {
		s.Apps = append([]string(nil), s.Apps...)
	}
	if s.Reasons != nil {
		s.Reasons = append([]string(nil), s.Reasons...)
	}
	return s
}

// SetPaused turns detection on or off without stopping the engine.
//
// Paused mode still runs the state machine and the publish/network gating —
// only the detectors are skipped, and the resolved state is forced to
// inactive. That reuses the same debounced, gated path a real "nobody is on
// a call" reading takes, rather than adding a second way to reach the bulb.
func (e *Engine) SetPaused(paused bool) { e.paused.Store(paused) }

// Paused reports whether detection is currently paused.
func (e *Engine) Paused() bool { return e.paused.Load() }

// Evaluate runs one poll cycle at the given time. Run supplies the real clock;
// tests supply their own.
func (e *Engine) Evaluate(ctx context.Context, now time.Time) { e.evaluate(ctx, now) }

func (e *Engine) evaluate(ctx context.Context, now time.Time) {
	resolved := detection.Resolved{State: model.StateInactive}
	if !e.paused.Load() {
		results := make([]model.DetectionResult, 0, len(e.detectors))
		for _, d := range e.detectors {
			results = append(results, d.Detect(ctx))
		}
		resolved = detection.Resolve(results)
	}

	e.refreshNetwork(ctx, now)

	state, changed := e.machine.Update(resolved, now)
	if changed {
		// A transition must survive an early return (network gate below, a
		// failed publish further down) and be retried on a later poll rather
		// than waiting for the next heartbeat.
		e.pending = true
	}

	e.statusMu.Lock()
	lastChange := e.status.LastChange
	if changed {
		lastChange = now
	}
	newStatus := Status{
		State:       state,
		App:         resolved.App,
		Apps:        resolved.Apps,
		Confidence:  resolved.Confidence,
		Reasons:     resolved.Reasons,
		Network:     e.netInfo,
		NetworkRule: e.netRule,
		Allowed:     e.netAllowed,
		LastChange:  lastChange,
		Paused:      e.paused.Load(),
		EvaluatedAt: now,
	}
	e.status = copyStatus(newStatus)
	e.evaluated = true
	e.statusMu.Unlock()
	if changed {
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
		e.pending = true
	}

	heartbeatDue := e.published && now.Sub(e.lastPublish) >= e.cfg.Poll.Heartbeat()

	// The first publish always happens, even though the state is still
	// unknown. The state topic is retained, so after a restart the broker is
	// still holding whatever was published before — possibly "active" from a
	// call that ended while the agent was down. Announcing "unknown" at once
	// clears that, instead of leaving the light red for a whole exit debounce
	// every time the agent restarts. That only works because the discovery
	// value_template (internal/mqtt/discovery.go) maps "unknown" to Home
	// Assistant's "None": a binary_sensor drops any payload it wasn't told
	// about, so an unmapped "unknown" would leave the stale "on" in place.
	startupAnnouncement := !e.published
	if startupAnnouncement {
		e.pending = true
	}

	if !e.pending && !heartbeatDue {
		return
	}

	payload := payloadFromStatus(e.cfg.App.DeviceID, newStatus)

	if err := e.publisher.PublishState(ctx, payload); err != nil {
		if !e.publishFailing {
			e.log.Warn("publish state, will retry", "error", err, "state", state)
			e.publishFailing = true
		}
		return
	}
	if e.publishFailing {
		e.log.Info("publish recovered", "state", state)
		e.publishFailing = false
	}
	e.published = true
	e.lastPublish = now
	e.pending = false
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

	infos, err := e.checker.Current(ctx)
	if err != nil {
		e.log.Warn("network check failed, suspending publishing", "error", err)
		e.netInfo, e.netRule, e.netAllowed = network.Info{}, "", false
		return
	}

	info, rule, allowed := e.matcher.MatchAny(infos)
	if allowed != e.netAllowed || rule != e.netRule {
		e.log.Info("network changed",
			"interface", info.Interface, "rule", rule, "allowed", allowed)
	}
	e.netInfo, e.netRule, e.netAllowed = info, rule, allowed
}
