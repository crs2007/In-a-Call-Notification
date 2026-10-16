package rules

import (
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
)

// Fixture-driven tests for the macOS rule set (rules_darwin.yaml). Fixtures
// live in testdata/probe/darwin/ and are parsed by loadFixture.

const (
	darwinInactiveThreshold = 0.30 // config default detection.inactive_threshold
	zoomMicBundleID         = "us.zoom.xos"
)

func darwinConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := DefaultFor("darwin")
	if err != nil {
		t.Fatalf("load darwin rules: %v", err)
	}
	return cfg
}

func evalDarwin(t *testing.T, cfg *Config, obs Observation, app string) model.DetectionResult {
	t.Helper()
	return find(t, cfg.Evaluate(obs, testThreshold, time.Now()), app)
}

func zoomWindow(title string) WindowObservation {
	return WindowObservation{Proc: "zoom.us", Title: title}
}

func TestDarwinZoomInCall_IsActive(t *testing.T) {
	cfg := darwinConfig(t)
	for i, obs := range loadFixture(t, "darwin/zoom-in-call.txt") {
		results := cfg.Evaluate(obs, testThreshold, time.Now())
		if got := find(t, results, "zoom"); got.State != model.StateActive {
			t.Errorf("snapshot %d: zoom state = %v, confidence = %v, want active", i, got.State, got.Confidence)
		}
		for _, app := range []string{"teams", "slack"} {
			if got := find(t, results, app); got.State != model.StateInactive || got.Confidence != 0 {
				t.Errorf("snapshot %d: %s state = %v, confidence = %v, want inactive/0", i, app, got.State, got.Confidence)
			}
		}
	}
}

// No Screen Recording permission: every title is empty. Process + mic alone
// must still reach the active threshold.
func TestDarwinZoomInCall_NoScreenRecording_StillActive(t *testing.T) {
	cfg := darwinConfig(t)
	for i, obs := range loadFixture(t, "darwin/zoom-in-call.txt") {
		blanked := obs
		blanked.Windows = nil
		for _, w := range obs.Windows {
			w.Title = ""
			blanked.Windows = append(blanked.Windows, w)
		}
		got := evalDarwin(t, cfg, blanked, "zoom")
		if got.State != model.StateActive {
			t.Errorf("snapshot %d: zoom state = %v, confidence = %v, want active on process + mic", i, got.State, got.Confidence)
		}
		for _, r := range got.Reasons {
			if r == "zoom: meeting window title matched" {
				t.Errorf("snapshot %d: window matched with blank titles", i)
			}
		}
	}
}

// Process + window with no mic must stay inactive: a title alone must not
// light the bulb.
func TestDarwinZoomInCall_NoMic_IsInactive(t *testing.T) {
	cfg := darwinConfig(t)
	for i, obs := range loadFixture(t, "darwin/zoom-in-call.txt") {
		obs.MicInUse = nil
		got := evalDarwin(t, cfg, obs, "zoom")
		if got.State != model.StateInactive || got.Confidence >= testThreshold {
			t.Errorf("snapshot %d: zoom state = %v, confidence = %v, want inactive", i, got.State, got.Confidence)
		}
	}
}

// S3, S7: idle, music and Zoom-open-never-joined must never score active for
// any app.
func TestDarwinNegativeFixtures_EveryAppInactive(t *testing.T) {
	cfg := darwinConfig(t)
	for _, name := range []string{"idle.txt", "music-playing.txt", "zoom-open-no-call.txt"} {
		for i, obs := range loadFixture(t, "darwin/"+name) {
			for _, got := range cfg.Evaluate(obs, testThreshold, time.Now()) {
				if got.State != model.StateInactive || got.Confidence >= testThreshold {
					t.Errorf("%s snapshot %d: %s state = %v, confidence = %v, want inactive", name, i, got.App, got.State, got.Confidence)
				}
			}
		}
	}
}

// The sign-in window must contribute process evidence only.
func TestDarwinZoomOpenNoCall_ProcessOnly(t *testing.T) {
	cfg := darwinConfig(t)
	for i, obs := range loadFixture(t, "darwin/zoom-open-no-call.txt") {
		got := evalDarwin(t, cfg, obs, "zoom")
		if got.Confidence > 0.20+1e-9 {
			t.Errorf("snapshot %d: zoom confidence = %v, want at most process-only 0.20", i, got.Confidence)
		}
	}
}

// Deliberate under-trigger: us.zoom.xos holding the mic with no zoom.us
// window at all (e.g. minimised) scores mic-only (0.50) and is not active.
func TestDarwinZoomMicOnly_IsInactive(t *testing.T) {
	cfg := darwinConfig(t)
	obs := Observation{
		Windows:  []WindowObservation{{Proc: "Finder", Title: "Downloads"}},
		MicInUse: []string{zoomMicBundleID},
	}
	got := evalDarwin(t, cfg, obs, "zoom")
	if got.State != model.StateInactive || got.Confidence >= testThreshold {
		t.Errorf("zoom state = %v, confidence = %v, want inactive on mic alone", got.State, got.Confidence)
	}
}

// Pins the weight sums documented in rules_darwin.yaml, including float
// rounding at the 0.70 boundary.
func TestDarwinZoomWeightSums(t *testing.T) {
	cfg := darwinConfig(t)
	meeting := zoomWindow("Zoom Meeting")
	cases := []struct {
		name   string
		obs    Observation
		want   float64
		active bool
	}{
		{"process only", Observation{Windows: []WindowObservation{zoomWindow("")}}, 0.20, false},
		{"mic only", Observation{MicInUse: []string{zoomMicBundleID}}, 0.50, false},
		{"process + window", Observation{Windows: []WindowObservation{meeting}}, 0.60, false},
		{"process + mic", Observation{Windows: []WindowObservation{zoomWindow("")}, MicInUse: []string{zoomMicBundleID}}, 0.70, true},
		{"all three", Observation{Windows: []WindowObservation{meeting}, MicInUse: []string{zoomMicBundleID}}, 1.00, true},
	}
	for _, c := range cases {
		got := evalDarwin(t, cfg, c.obs, "zoom")
		if d := got.Confidence - c.want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s: confidence = %v, want %v", c.name, got.Confidence, c.want)
		}
		if (got.State == model.StateActive) != c.active {
			t.Errorf("%s: state = %v (confidence %v), want active=%v", c.name, got.State, got.Confidence, c.active)
		}
	}
	// Hysteresis: process only must fall below the inactive threshold, and
	// process + window must stay above it so a dropped mic does not flip an
	// active call.
	if got := evalDarwin(t, cfg, cases[0].obs, "zoom"); got.Confidence >= darwinInactiveThreshold {
		t.Errorf("process only = %v, want below inactive threshold %v", got.Confidence, darwinInactiveThreshold)
	}
	if got := evalDarwin(t, cfg, cases[2].obs, "zoom"); got.Confidence < darwinInactiveThreshold {
		t.Errorf("process + window = %v, want at least inactive threshold %v", got.Confidence, darwinInactiveThreshold)
	}
}

// Workplace (home window) and Login must never count as a meeting window.
func TestDarwinZoomNonMeetingTitles_NoWindowEvidence(t *testing.T) {
	cfg := darwinConfig(t)
	for _, title := range []string{"Zoom Workplace", "Login", "", "Zoom Meeting extra", "zoom meeting"} {
		got := evalDarwin(t, cfg, Observation{Windows: []WindowObservation{zoomWindow(title)}}, "zoom")
		if got.Confidence > 0.20+1e-9 {
			t.Errorf("title %q: confidence = %v, want process-only", title, got.Confidence)
		}
	}
	// A browser window is never Zoom evidence.
	chrome := Observation{Windows: []WindowObservation{{Proc: "Google Chrome", Title: "Zoom Meeting"}}}
	if got := evalDarwin(t, cfg, chrome, "zoom"); got.Confidence != 0 {
		t.Errorf("chrome window: confidence = %v, want 0", got.Confidence)
	}
}

// The only captured webcam entry is "(unknown)"; it must add nothing.
func TestDarwinCameraUnknown_ContributesNothing(t *testing.T) {
	cfg := darwinConfig(t)
	obs := Observation{
		Windows:  []WindowObservation{zoomWindow("Zoom Meeting")},
		MicInUse: []string{zoomMicBundleID},
	}
	base := evalDarwin(t, cfg, obs, "zoom")
	obs.CamInUse = []string{"(unknown)"}
	for _, r := range cfg.Evaluate(obs, testThreshold, time.Now()) {
		if r.App == "zoom" && r.Confidence != base.Confidence {
			t.Errorf("zoom confidence changed with camera: %v -> %v", base.Confidence, r.Confidence)
		}
	}
	// Camera alone, with every app's process open, scores nothing extra.
	camOnly := Observation{
		Windows:  []WindowObservation{zoomWindow(""), {Proc: "Microsoft Teams", Title: ""}, {Proc: "Slack", Title: ""}},
		CamInUse: []string{"(unknown)"},
	}
	for _, r := range cfg.Evaluate(camOnly, testThreshold, time.Now()) {
		want := 0.0
		if r.App == "zoom" {
			want = 0.20
		}
		if d := r.Confidence - want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s: confidence with camera only = %v, want %v", r.App, r.Confidence, want)
		}
	}
}

// Teams and Slack have no captured macOS fixtures, hence no patterns: they
// score 0 whatever the observation holds.
func TestDarwinTeamsSlack_NoPatternsScoreZero(t *testing.T) {
	cfg := darwinConfig(t)
	obs := Observation{
		Windows: []WindowObservation{
			{Proc: "MSTeams", Title: "Meeting with X | Microsoft Teams"},
			{Proc: "Microsoft Teams", Title: "Weekly | Microsoft Teams"},
			{Proc: "Slack", Title: "huddle \U0001F50A"},
		},
		MicInUse: []string{"com.microsoft.teams2", "com.tinyspeck.slackmacgap", zoomMicBundleID},
		CamInUse: []string{"(unknown)"},
	}
	for _, app := range []string{"teams", "slack"} {
		if got := evalDarwin(t, cfg, obs, app); got.Confidence != 0 || got.State != model.StateInactive {
			t.Errorf("%s: state = %v, confidence = %v, want inactive/0", app, got.State, got.Confidence)
		}
	}
	for _, fixture := range []string{"zoom-in-call.txt", "music-playing.txt"} {
		for i, o := range loadFixture(t, "darwin/"+fixture) {
			for _, app := range []string{"teams", "slack"} {
				if got := evalDarwin(t, cfg, o, app); got.Confidence != 0 {
					t.Errorf("%s snapshot %d: %s confidence = %v, want 0", fixture, i, app, got.Confidence)
				}
			}
		}
	}
}
