package detectors

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/rules"
	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

const testRulesYAML = `
rules:
  - app: teams
    process_names: ["ms-teams.exe"]
    window_include_regex: ["Meeting"]
    mic_process_regex: ["ms-teams.exe"]
    weights:
      process: 0.2
      window: 0.6
      mic: 0.3
  - app: zoom
    process_names: ["zoom.exe"]
    window_include_regex: ["Zoom Meeting"]
    mic_process_regex: ["zoom.exe"]
    weights:
      process: 0.2
      window: 0.6
      mic: 0.3
`

func loadTestConfig(t *testing.T) *rules.Config {
	t.Helper()
	cfg, err := rules.Load([]byte(testRulesYAML))
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}
	return cfg
}

// fakeSnapshot builds a Snapshot from fixed values, with no platform calls.
func fakeSnapshot(windows []platformwindows.WindowInfo, procNames map[uint32]string, mic, cam []string) Snapshot {
	return Snapshot{
		VisibleWindows:      func() []platformwindows.WindowInfo { return windows },
		ProcessNames:        func() map[uint32]string { return procNames },
		AppsUsingMicrophone: func(map[uint32]string) []string { return mic },
		AppsUsingWebcam:     func(map[uint32]string) []string { return cam },
	}
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// allEnabled is the enabled-check most tests in this file want: every rule
// in cfg gets a detector.
func allEnabled(string) bool { return true }

func TestNew_FullObservationCrossesThreshold(t *testing.T) {
	cfg := loadTestConfig(t)
	snap := fakeSnapshot(
		[]platformwindows.WindowInfo{{PID: 1, Title: "Meeting with Bob"}},
		map[uint32]string{1: "ms-teams.exe"},
		[]string{"ms-teams.exe"},
		nil,
	)
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	dets := New(cfg, 0.5, 0.3, allEnabled, snap, fixedNow(now))
	if len(dets) != 2 {
		t.Fatalf("got %d detectors, want 2", len(dets))
	}

	var teams model.Detector
	for _, d := range dets {
		if d.App() == "teams" {
			teams = d
		}
	}
	if teams == nil {
		t.Fatalf("no teams detector found")
	}

	result := teams.Detect(context.Background())
	if result.App != "teams" {
		t.Errorf("App = %q, want teams", result.App)
	}
	if result.State != model.StateActive {
		t.Errorf("State = %v, want active (confidence %v)", result.State, result.Confidence)
	}
	if !result.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", result.Timestamp, now)
	}
}

func TestNew_NoSignalsIsInactiveWithZeroConfidence(t *testing.T) {
	cfg := loadTestConfig(t)
	snap := fakeSnapshot(nil, nil, nil, nil)
	now := time.Now()

	dets := New(cfg, 0.5, 0.3, allEnabled, snap, fixedNow(now))
	for _, d := range dets {
		result := d.Detect(context.Background())
		if result.State != model.StateInactive {
			t.Errorf("app %s: State = %v, want inactive", d.App(), result.State)
		}
		if result.Confidence != 0 {
			t.Errorf("app %s: Confidence = %v, want 0", d.App(), result.Confidence)
		}
	}
}

func TestNew_MultipleRulesEachProduceOwnResult(t *testing.T) {
	cfg := loadTestConfig(t)
	snap := fakeSnapshot(
		[]platformwindows.WindowInfo{
			{PID: 1, Title: "Meeting with Bob"},
			{PID: 2, Title: "Zoom Meeting in progress"},
		},
		map[uint32]string{1: "ms-teams.exe", 2: "zoom.exe"},
		[]string{"ms-teams.exe", "zoom.exe"},
		nil,
	)
	now := time.Now()

	dets := New(cfg, 0.5, 0.3, allEnabled, snap, fixedNow(now))
	if len(dets) != 2 {
		t.Fatalf("got %d detectors, want 2", len(dets))
	}

	seen := map[string]model.DetectionResult{}
	for _, d := range dets {
		seen[d.App()] = d.Detect(context.Background())
	}

	for _, app := range []string{"teams", "zoom"} {
		r, ok := seen[app]
		if !ok {
			t.Fatalf("no result for app %q", app)
		}
		if r.App != app {
			t.Errorf("result for %q has App = %q", app, r.App)
		}
		if r.State != model.StateActive {
			t.Errorf("app %q: State = %v, want active", app, r.State)
		}
	}
}

func TestDetector_ImplementsModelDetector(t *testing.T) {
	cfg := loadTestConfig(t)
	snap := fakeSnapshot(nil, nil, nil, nil)
	dets := New(cfg, 0.5, 0.3, allEnabled, snap, fixedNow(time.Now()))
	for _, d := range dets {
		var _ model.Detector = d
		// ctx is ignored by design (no platform call here takes one); a
		// cancelled context must not change the result or panic.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = d.Detect(ctx)
	}
}

func TestNew_DisabledAppSkipped(t *testing.T) {
	const threeAppRulesYAML = `
rules:
  - app: teams
    process_names: ["ms-teams.exe"]
    window_include_regex: ["Meeting"]
    weights:
      process: 0.2
      window: 0.6
  - app: zoom
    process_names: ["zoom.exe"]
    window_include_regex: ["Zoom Meeting"]
    weights:
      process: 0.2
      window: 0.6
  - app: slack
    process_names: ["slack.exe"]
    window_include_regex: ["Huddle"]
    weights:
      process: 0.2
      window: 0.6
`
	cfg, err := rules.Load([]byte(threeAppRulesYAML))
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}
	snap := fakeSnapshot(nil, nil, nil, nil)
	disableZoom := func(app string) bool { return app != "zoom" }

	dets := New(cfg, 0.5, 0.3, disableZoom, snap, fixedNow(time.Now()))
	if len(dets) != 2 {
		t.Fatalf("got %d detectors, want 2", len(dets))
	}
	for _, d := range dets {
		if d.App() == "zoom" {
			t.Errorf("zoom detector present despite being disabled")
		}
	}
}

// TestDetect_HysteresisHoldsActiveBetweenThresholds walks one detector's
// confidence 0.85 -> 0.60 -> 0.25 against a fixture rule crossing the
// default active (0.70) and inactive (0.30) thresholds, and checks the
// "Teams generic title without mic" flicker never reads Inactive until
// confidence actually drops below inactiveThreshold.
//
// The middle step (0.60) uses a mic-only match rather than a window-only
// one: in rules.go, hasWindowMatch requires the matching window's own
// process to be in ProcessNames, which is exactly what hasProcess also
// checks, so any observation that credits Window necessarily credits
// Process too (0.25+0.60=0.85, never 0.60 alone). Mic evidence has no such
// coupling (matchesAny only looks at the mic-in-use list), so it is what
// isolates the middle confidence value here.
func TestDetect_HysteresisHoldsActiveBetweenThresholds(t *testing.T) {
	const hysteresisRuleYAML = `
rules:
  - app: teams
    process_names: ["ms-teams.exe"]
    window_include_regex: ["Meeting"]
    mic_process_regex: ["ms-teams.exe"]
    weights:
      process: 0.25
      window: 0.60
      mic: 0.60
`
	cfg, err := rules.Load([]byte(hysteresisRuleYAML))
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	meetingWindow := platformwindows.WindowInfo{PID: 1, Title: "Meeting with Bob"}
	process := platformwindows.WindowInfo{PID: 1, Title: "ms-teams.exe window"}
	procNames := map[uint32]string{1: "ms-teams.exe"}

	// obsA: process present + matching window title -> 0.25+0.60 = 0.85.
	obsA := fakeSnapshot([]platformwindows.WindowInfo{meetingWindow}, procNames, nil, nil)
	// obsB: microphone in use by teams only, no window or process -> 0.60.
	obsB := fakeSnapshot(nil, nil, []string{"ms-teams.exe"}, nil)
	// obsC: process present only, no window match, no mic -> 0.25.
	obsC := fakeSnapshot([]platformwindows.WindowInfo{process}, procNames, nil, nil)

	// clock is a mutable clock, advanced between steps below so each one
	// lands outside sharedObserverTTL and is treated as a genuinely new poll
	// rather than replaying the cached Observation from the previous step.
	// The real engine gets this for free from time.Now() advancing between
	// ticks (see cmd/callmqtt/main.go's buildDetectors call); a frozen
	// injected clock has to fake that advance explicitly.
	clock := time.Now()
	now := func() time.Time { return clock }

	dets := New(cfg, 0.70, 0.30, allEnabled, obsA, now)
	if len(dets) != 1 {
		t.Fatalf("got %d detectors, want 1", len(dets))
	}
	d, ok := dets[0].(*Detector)
	if !ok {
		t.Fatalf("detector is %T, want *Detector", dets[0])
	}

	steps := []struct {
		name       string
		snap       Snapshot
		wantState  model.CallState
		wantConfid float64
	}{
		{"obsA", obsA, model.StateActive, 0.85},
		{"obsB", obsB, model.StateActive, 0.60},
		{"obsC", obsC, model.StateInactive, 0.25},
	}

	for _, step := range steps {
		clock = clock.Add(time.Second)
		d.snapshot = step.snap
		result := d.Detect(context.Background())
		if result.Confidence != step.wantConfid {
			t.Errorf("%s: Confidence = %v, want %v", step.name, result.Confidence, step.wantConfid)
		}
		if result.State != step.wantState {
			t.Errorf("%s: State = %v, want %v", step.name, result.State, step.wantState)
		}
	}
}

// TestNew_SharesOneObservationAcrossDetectorsInOnePoll is the regression
// test for TODO 5.1: three detectors built by one New call, each Detect
// called once (one poll sweep), must together trigger every platform
// function exactly once, not once per detector.
func TestNew_SharesOneObservationAcrossDetectorsInOnePoll(t *testing.T) {
	const threeAppRulesYAML = `
rules:
  - app: teams
    process_names: ["ms-teams.exe"]
    window_include_regex: ["Meeting"]
    weights:
      process: 0.2
      window: 0.6
  - app: zoom
    process_names: ["zoom.exe"]
    window_include_regex: ["Zoom Meeting"]
    weights:
      process: 0.2
      window: 0.6
  - app: slack
    process_names: ["slack.exe"]
    window_include_regex: ["Huddle"]
    weights:
      process: 0.2
      window: 0.6
`
	cfg, err := rules.Load([]byte(threeAppRulesYAML))
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	var windowsCalls, procCalls, micCalls, camCalls int32
	snap := Snapshot{
		VisibleWindows: func() []platformwindows.WindowInfo {
			atomic.AddInt32(&windowsCalls, 1)
			return nil
		},
		ProcessNames: func() map[uint32]string {
			atomic.AddInt32(&procCalls, 1)
			return nil
		},
		AppsUsingMicrophone: func(map[uint32]string) []string {
			atomic.AddInt32(&micCalls, 1)
			return nil
		},
		AppsUsingWebcam: func(map[uint32]string) []string {
			atomic.AddInt32(&camCalls, 1)
			return nil
		},
	}

	dets := New(cfg, 0.5, 0.3, allEnabled, snap, fixedNow(time.Now()))
	if len(dets) != 3 {
		t.Fatalf("got %d detectors, want 3", len(dets))
	}

	// One poll sweep: the engine calls Detect on every detector in turn.
	for _, d := range dets {
		d.Detect(context.Background())
	}

	for name, got := range map[string]int32{
		"VisibleWindows":      atomic.LoadInt32(&windowsCalls),
		"ProcessNames":        atomic.LoadInt32(&procCalls),
		"AppsUsingMicrophone": atomic.LoadInt32(&micCalls),
		"AppsUsingWebcam":     atomic.LoadInt32(&camCalls),
	} {
		if got != 1 {
			t.Errorf("%s called %d times across 3 detectors' one poll sweep, want 1", name, got)
		}
	}
}

func TestWindowsSnapshot_ReturnsCallablePlatformFunctions(t *testing.T) {
	snap := WindowsSnapshot()
	if snap.VisibleWindows == nil || snap.ProcessNames == nil ||
		snap.AppsUsingMicrophone == nil || snap.AppsUsingWebcam == nil {
		t.Fatal("WindowsSnapshot left a nil field")
	}
	// Calling these must not panic even on a platform without the real
	// implementation (the !windows stub returns nil).
	_ = snap.VisibleWindows()
	names := snap.ProcessNames()
	_ = snap.AppsUsingMicrophone(names)
	_ = snap.AppsUsingWebcam(names)
}
