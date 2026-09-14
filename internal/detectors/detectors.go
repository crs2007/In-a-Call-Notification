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
	"time"

	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/rules"
	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

// Snapshot is every platform call Detect needs, injected so tests can fake
// "these windows are visible" without EnumWindows, the registry, or any OS
// permission.
type Snapshot struct {
	VisibleWindows      func() []platformwindows.WindowInfo
	ProcessNames        func() map[uint32]string
	AppsUsingMicrophone func() []string
	AppsUsingWebcam     func() []string
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
		MicInUse: s.AppsUsingMicrophone(),
		CamInUse: s.AppsUsingWebcam(),
	}
	for _, w := range wins {
		obs.Windows = append(obs.Windows, rules.WindowObservation{
			Proc:  names[w.PID],
			Title: w.Title,
		})
	}
	return obs
}

// Detector implements model.Detector for exactly one app rule, scoring a
// fresh Snapshot of the OS on every call. This matches how internal/engine
// consumes detectors: one model.Detector per app, so internal/detection can
// resolve them independently.
type Detector struct {
	rule            rules.CompiledRule
	snapshot        Snapshot
	activeThreshold float64
	now             func() time.Time
}

// App implements model.Detector.
func (d *Detector) App() string { return d.rule.App }

// Detect implements model.Detector. It never errors: a platform call that
// fails contributes no evidence rather than aborting the poll, exactly as
// the injected Snapshot functions themselves guarantee.
func (d *Detector) Detect(_ context.Context) model.DetectionResult {
	obs := d.snapshot.observe()
	return d.rule.Evaluate(obs, d.activeThreshold, d.now())
}

// New builds one model.Detector per rule in cfg, all reading the same
// Snapshot of platform signals on every poll.
//
// activeThreshold and now are injected rather than read from a package-level
// clock or config, so the whole chain stays testable in microseconds and
// tunable without touching this package.
func New(cfg *rules.Config, activeThreshold float64, snapshot Snapshot, now func() time.Time) []model.Detector {
	out := make([]model.Detector, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		out = append(out, &Detector{
			rule:            r,
			snapshot:        snapshot,
			activeThreshold: activeThreshold,
			now:             now,
		})
	}
	return out
}
