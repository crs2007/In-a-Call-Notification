// Package detectors is the adapter between platform/windows and the pure
// rule engine in internal/rules: it gathers raw OS signals (visible windows,
// process names, microphone/webcam consent) into a rules.Observation and
// hands it to a compiled rule to score.
//
// Every other package under internal/ (rules, detection, engine, model)
// never imports platform/ — dependencies point inward, and platform code is
// injected as an interface the consumer defines. This package is the
// deliberate, one-place exception: its entire job is turning platform input
// into the model the rest of the tree consumes, the same role internal/mqtt
// plays for output. Nothing downstream of detectors ever needs to know a
// syscall exists.
package detectors

import (
	"context"
	"sync"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/rules"
	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

// sharedObserverTTL is how long one platform observation is reused across
// detectors built by the same New call. It only needs to outlast a single
// poll sweep (all detectors' Detect calls happen back to back on one engine
// tick), so it is measured in wall-clock time rather than a tick counter:
// there is no tick counter to key on down here, and time.Since is simpler
// than threading one through model.Detector.
const sharedObserverTTL = 500 * time.Millisecond

// Snapshot is every platform call Detect needs, injected so tests can fake
// "these windows are visible" without EnumWindows, the registry, or any OS
// permission.
type Snapshot struct {
	VisibleWindows func() []platformwindows.WindowInfo
	ProcessNames   func() map[uint32]string
	// AppsUsingMicrophone and AppsUsingWebcam take the same PID->exe-name map
	// ProcessNames just returned, so they can cross-check a ConsentStore
	// entry's owning process is actually still running without taking a
	// second, redundant process snapshot. observe already has that map in
	// hand by the time it calls these.
	AppsUsingMicrophone func(procNames map[uint32]string) []string
	AppsUsingWebcam     func(procNames map[uint32]string) []string
}

// WindowsSnapshot returns a Snapshot backed by the real platform/windows
// implementation (or its no-op stub on non-Windows builds).
func WindowsSnapshot() Snapshot {
	return Snapshot{
		VisibleWindows:      platformwindows.VisibleWindows,
		ProcessNames:        platformwindows.ProcessNames,
		AppsUsingMicrophone: platformwindows.AppsUsingMicrophone,
		AppsUsingWebcam:     platformwindows.AppsUsingWebcam,
	}
}

// observe gathers one rules.Observation from the platform, pairing each
// visible window's PID with its owning process name. A window whose process
// can't be named still counts as a window with an empty Proc, which simply
// never matches any rule's ProcessNames.
func (s Snapshot) observe() rules.Observation {
	names := s.ProcessNames()
	wins := s.VisibleWindows()

	obs := rules.Observation{
		Windows:  make([]rules.WindowObservation, 0, len(wins)),
		MicInUse: s.AppsUsingMicrophone(names),
		CamInUse: s.AppsUsingWebcam(names),
	}
	for _, w := range wins {
		obs.Windows = append(obs.Windows, rules.WindowObservation{
			Proc:  names[w.PID],
			Title: w.Title,
		})
	}
	return obs
}

// sharedObserver caches one rules.Observation across every Detector built by
// the same New call, so a poll with N enabled apps observes the platform
// once instead of N times.
//
// Nothing calls Detect concurrently today, but the mutex is cheap and this
// is exactly the kind of "two calls land on the same tick from two
// goroutines someday" concern platform/windows.enumMu already guards against
// elsewhere in this codebase, so the same defensive habit applies here.
type sharedObserver struct {
	now func() time.Time
	ttl time.Duration

	mu       sync.Mutex
	last     rules.Observation
	lastAt   time.Time
	hasCache bool
}

// observe returns the cached Observation if it is younger than ttl,
// otherwise it takes a fresh one via snapshot and caches that instead.
// snapshot is passed in rather than stored so that tests can still swap a
// single Detector's Snapshot between calls (see detectors_test.go's
// hysteresis test) without needing a second observer per Detector.
func (o *sharedObserver) observe(snapshot Snapshot) rules.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.now()
	if o.hasCache && now.Sub(o.lastAt) < o.ttl {
		return o.last
	}
	o.last = snapshot.observe()
	o.lastAt = now
	o.hasCache = true
	return o.last
}

// Detector implements model.Detector for exactly one app rule, scoring an
// Observation shared with every other Detector from the same New call. This
// matches how internal/engine consumes detectors: one model.Detector per
// app, so internal/detection can resolve them independently, without each
// one re-observing the platform from scratch.
type Detector struct {
	rule              rules.CompiledRule
	snapshot          Snapshot
	shared            *sharedObserver
	activeThreshold   float64
	inactiveThreshold float64
	now               func() time.Time

	// wasActive carries the previous Detect result across polls, so a rule
	// that dipped below activeThreshold but not below inactiveThreshold (the
	// "Teams generic title without mic" flicker) is still reported active
	// instead of bouncing state every poll.
	wasActive bool
}

// App implements model.Detector.
func (d *Detector) App() string { return d.rule.App }

// Detect implements model.Detector. It never errors: a platform call that
// fails contributes no evidence rather than aborting the poll, exactly as
// the injected Snapshot functions themselves guarantee.
func (d *Detector) Detect(_ context.Context) model.DetectionResult {
	obs := d.shared.observe(d.snapshot)
	result := d.rule.Evaluate(obs, d.activeThreshold, d.now())

	if d.wasActive && result.State == model.StateInactive && result.Confidence >= d.inactiveThreshold {
		result.State = model.StateActive
	}
	d.wasActive = result.State == model.StateActive

	return result
}

// New builds one model.Detector per rule in cfg whose app the enabled check
// approves, all reading the same Snapshot of platform signals on every poll.
// Every Detector built by this call shares one sharedObserver, so a poll
// sweep that calls Detect on all of them still observes the platform once
// per tick rather than once per detector.
//
// activeThreshold, inactiveThreshold and now are injected rather than read
// from a package-level clock or config, so the whole chain stays testable in
// microseconds and tunable without touching this package.
func New(cfg *rules.Config, activeThreshold, inactiveThreshold float64, enabled func(app string) bool, snapshot Snapshot, now func() time.Time) []model.Detector {
	shared := &sharedObserver{now: now, ttl: sharedObserverTTL}

	out := make([]model.Detector, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		if !enabled(r.App) {
			continue
		}
		out = append(out, &Detector{
			rule:              r,
			snapshot:          snapshot,
			shared:            shared,
			activeThreshold:   activeThreshold,
			inactiveThreshold: inactiveThreshold,
			now:               now,
		})
	}
	return out
}
