package detectors

import (
	"context"
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
		AppsUsingMicrophone: func() []string { return mic },
		AppsUsingWebcam:     func() []string { return cam },
	}
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestNew_FullObservationCrossesThreshold(t *testing.T) {
	cfg := loadTestConfig(t)
	snap := fakeSnapshot(
		[]platformwindows.WindowInfo{{PID: 1, Title: "Meeting with Bob"}},
		map[uint32]string{1: "ms-teams.exe"},
		[]string{"ms-teams.exe"},
		nil,
	)
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	dets := New(cfg, 0.5, snap, fixedNow(now))
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

	dets := New(cfg, 0.5, snap, fixedNow(now))
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

	dets := New(cfg, 0.5, snap, fixedNow(now))
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
	dets := New(cfg, 0.5, snap, fixedNow(time.Now()))
	for _, d := range dets {
		var _ model.Detector = d
		// ctx is ignored by design (no platform call here takes one); a
		// cancelled context must not change the result or panic.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = d.Detect(ctx)
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
	_ = snap.ProcessNames()
	_ = snap.AppsUsingMicrophone()
	_ = snap.AppsUsingWebcam()
}
