package rules

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
)

// -----------------------------------------------------------------------
// Fixture parsing: turns a testdata/probe/*.txt capture into a sequence of
// Observations, one per "=== timestamp ===" snapshot in the file.
// -----------------------------------------------------------------------

var windowLineRe = regexp.MustCompile(`^\s*pid=\S+\s+proc=(.*?)\s+title=(.*)$`)

func loadFixture(t *testing.T, name string) []Observation {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "probe", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()

	var snapshots []Observation
	var cur *Observation
	section := ""

	flush := func() {
		if cur != nil {
			snapshots = append(snapshots, *cur)
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "=== "):
			flush()
			cur = &Observation{}
			section = ""
			continue
		case strings.HasPrefix(line, "[windows]"):
			section = "windows"
			continue
		case strings.HasPrefix(line, "[microphone]"):
			section = "mic"
			continue
		case strings.HasPrefix(line, "[webcam]"):
			section = "cam"
			continue
		case strings.HasPrefix(line, "[audio-out]"):
			// Probe instrumentation only (see cmd/probe): no rule scores it,
			// so its lines are skipped rather than carried into Observation.
			section = "audio"
			continue
		case strings.TrimSpace(line) == "":
			continue
		}

		if cur == nil {
			continue
		}

		switch section {
		case "windows":
			m := windowLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			title, err := strconv.Unquote(m[2])
			if err != nil {
				t.Fatalf("fixture %s: unquote title %q: %v", name, m[2], err)
			}
			cur.Windows = append(cur.Windows, WindowObservation{Proc: m[1], Title: title})
		case "mic":
			cur.MicInUse = append(cur.MicInUse, strings.TrimSpace(line))
		case "cam":
			cur.CamInUse = append(cur.CamInUse, strings.TrimSpace(line))
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture %s: %v", name, err)
	}
	if len(snapshots) == 0 {
		t.Fatalf("fixture %s: no snapshots parsed", name)
	}
	return snapshots
}

// find returns the DetectionResult for app within a slice of results.
func find(t *testing.T, results []model.DetectionResult, app string) model.DetectionResult {
	t.Helper()
	for _, r := range results {
		if r.App == app {
			return r
		}
	}
	t.Fatalf("no result for app %q", app)
	return model.DetectionResult{}
}

const testThreshold = 0.70

func liveConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := Default()
	if err != nil {
		t.Fatalf("load default rules: %v", err)
	}
	return cfg
}

// TestDefault_MatchesShippedFile guards against the embed and the on-disk
// file drifting apart, which //go:embed rules.yaml can't do by construction
// but is worth asserting explicitly since both are read in this package.
func TestDefault_MatchesShippedFile(t *testing.T) {
	fromEmbed, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	fromFile, err := LoadFile(filepath.Join("rules.yaml"))
	if err != nil {
		t.Fatalf("LoadFile(rules.yaml): %v", err)
	}
	if len(fromEmbed.Rules) != len(fromFile.Rules) {
		t.Fatalf("got %d embedded rules, want %d (from file)", len(fromEmbed.Rules), len(fromFile.Rules))
	}
}

// -----------------------------------------------------------------------
// Loader tests
// -----------------------------------------------------------------------

func TestLoad_ShippedRulesFile(t *testing.T) {
	cfg := liveConfig(t)
	if len(cfg.Rules) != 4 {
		t.Fatalf("got %d rules, want 4", len(cfg.Rules))
	}
	apps := map[string]bool{}
	for _, r := range cfg.Rules {
		apps[r.App] = true
	}
	for _, want := range []string{"teams", "zoom", "slack", "meet"} {
		if !apps[want] {
			t.Errorf("missing rule for app %q", want)
		}
	}
}

func TestLoad_BadRegexIsAConfigError(t *testing.T) {
	doc := []byte(`
rules:
  - app: broken
    process_names: [broken.exe]
    window_include_regex:
      - "(unclosed"
    weights:
      process: 0.2
      window: 0.5
      mic: 0.3
`)
	_, err := Load(doc)
	if err == nil {
		t.Fatal("expected an error for an invalid regex, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"broken", "window_include_regex", "unclosed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestLoad_BadYAMLIsAConfigError(t *testing.T) {
	_, err := Load([]byte("not: [valid"))
	if err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

func TestLoadFile_MissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join("testdata", "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

// -----------------------------------------------------------------------
// Fixture-driven scoring: the acceptance-bar scenarios.
// -----------------------------------------------------------------------

// S1/S2: a real Teams call, on. Every snapshot must resolve to active.
func TestTeamsInCall_IsActive(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "teams-in-call.txt")
	for i, obs := range snaps {
		results := cfg.Evaluate(obs, testThreshold, time.Now())
		got := find(t, results, "teams")
		if got.State != model.StateActive {
			t.Errorf("snapshot %d: teams state = %v, confidence = %v, want active", i, got.State, got.Confidence)
		}
	}
}

func TestTeamsInCallGenericTitle_RequiresMic(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "teams-in-call-generic-title.txt")
	for i, obs := range snaps {
		got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "teams")
		if got.State != model.StateActive {
			t.Errorf("snapshot %d: teams state = %v, confidence = %v, want active", i, got.State, got.Confidence)
		}

		obs.MicInUse = nil
		got = find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "teams")
		if got.State != model.StateInactive {
			t.Errorf("snapshot %d without mic: teams state = %v, confidence = %v, want inactive", i, got.State, got.Confidence)
		}
		if got.Confidence >= testThreshold {
			t.Errorf("snapshot %d without mic: teams confidence = %v crossed the active threshold", i, got.Confidence)
		}
	}
}

// S4: muting/unmuting Teams must cause literally no state or score change,
// because the new (MSIX) Teams client exposes no distinguishable mute-state
// signal in the window title or the mic-in-use entry.
func TestTeamsMuteUnmute_NoStateChange(t *testing.T) {
	cfg := liveConfig(t)
	unmuted := loadFixture(t, "teams-in-call.txt")
	muted := loadFixture(t, "teams-in-call-muted.txt")

	unmutedResult := find(t, cfg.Evaluate(unmuted[0], testThreshold, time.Now()), "teams")
	mutedResult := find(t, cfg.Evaluate(muted[0], testThreshold, time.Now()), "teams")

	if unmutedResult.State != model.StateActive || mutedResult.State != model.StateActive {
		t.Fatalf("expected both muted and unmuted to be active, got unmuted=%v muted=%v",
			unmutedResult.State, mutedResult.State)
	}
	if unmutedResult.Confidence != mutedResult.Confidence {
		t.Errorf("mute/unmute changed confidence: unmuted=%v muted=%v",
			unmutedResult.Confidence, mutedResult.Confidence)
	}

	// Every muted snapshot, individually, must also be active - mute is not
	// a state that flickers in and out.
	for i, obs := range muted {
		got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "teams")
		if got.State != model.StateActive {
			t.Errorf("muted snapshot %d: teams state = %v, want active", i, got.State)
		}
	}
}

// S8: a Teams chat/calendar window, with the mic idle, must never register
// as an active call - regardless of how long it stays open.
func TestTeamsOpenNoCall_IsInactive(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "teams-open-no-call.txt")
	for i, obs := range snaps {
		got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "teams")
		if got.State != model.StateInactive {
			t.Errorf("snapshot %d: teams state = %v, confidence = %v, want inactive", i, got.State, got.Confidence)
		}
		if got.Confidence >= testThreshold {
			t.Errorf("snapshot %d: teams confidence = %v crossed the active threshold on a chat window", i, got.Confidence)
		}
	}
}

// Regression for a confirmed live false positive (2026-09-14): new Teams
// always keeps a small utility window titled exactly "Meet | Microsoft
// Teams" open, even when completely idle with no call in progress and 0
// apps using the microphone (`go run ./cmd/probe --count 1` on a real
// machine showed only pid=63580 proc=ms-teams.exe title="Meet | Microsoft
// Teams", no "Meeting with ..." window, and no mic entries).
// testdata/probe/teams-open-no-call.txt never exercised this because it was
// captured on the Chat tab, so this is synthesized inline rather than
// requiring a new capture. Must never cross the active threshold.
func TestTeamsIdleMeetWindowOnly_IsInactive(t *testing.T) {
	cfg := liveConfig(t)
	obs := Observation{
		Windows: []WindowObservation{
			{Proc: "ms-teams.exe", Title: "Meet | Microsoft Teams"},
		},
		// No mic entries: confirmed 0 apps using the microphone.
	}
	got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "teams")
	if got.State != model.StateInactive {
		t.Errorf("teams state = %v, confidence = %v, want inactive", got.State, got.Confidence)
	}
	if got.Confidence >= testThreshold {
		t.Errorf("teams confidence = %v crossed the active threshold on the bare idle \"Meet\" window alone", got.Confidence)
	}
}

// S1/S2: a real Zoom call, on. Every snapshot must resolve to active.
func TestZoomInCall_IsActive(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "zoom-in-call.txt")
	for i, obs := range snaps {
		got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "zoom")
		if got.State != model.StateActive {
			t.Errorf("snapshot %d: zoom state = %v, confidence = %v, want active", i, got.State, got.Confidence)
		}
	}
}

// S3 (release-blocking): Zoom open all day, never joined -> inactive, and
// specifically never crosses the active threshold, in every snapshot.
func TestZoomOpenNoCall_IsInactive(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "zoom-open-no-call.txt")
	for i, obs := range snaps {
		got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "zoom")
		if got.State != model.StateInactive {
			t.Errorf("snapshot %d: zoom state = %v, confidence = %v, want inactive", i, got.State, got.Confidence)
		}
		if got.Confidence >= testThreshold {
			t.Errorf("snapshot %d: zoom confidence = %v crossed the active threshold with no meeting joined", i, got.Confidence)
		}
	}
}

// Slack huddle: the speaker marker is intermittent (see rules.yaml). This
// fixture must show at least one active snapshot (detection is possible),
// but must not be active on every snapshot (the marker's absence in most
// samples must not be papered over), and the mic-plus-process combination
// alone (no marker) must stay below the active threshold.
func TestSlackHuddle_DetectsIntermittently(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "slack-huddle.txt")

	activeCount := 0
	for _, obs := range snaps {
		got := find(t, cfg.Evaluate(obs, testThreshold, time.Now()), "slack")
		if got.State == model.StateActive {
			activeCount++
			continue
		}
		// Every inactive snapshot in this fixture still has the process
		// present and the microphone held by Slack; confirm that
		// combination alone never reaches the active threshold on its own.
		if got.Confidence >= testThreshold {
			t.Errorf("slack confidence %v reached the active threshold without the huddle marker", got.Confidence)
		}
	}

	if activeCount == 0 {
		t.Error("expected at least one snapshot with the huddle speaker marker to be active")
	}
	if activeCount == len(snaps) {
		t.Error("expected the intermittent marker to leave at least one snapshot inactive")
	}
}

// S7 (release-blocking): music playing, no call app running at all -> every
// rule stays inactive with zero confidence, in every snapshot.
func TestMusicPlaying_IsInactiveForEveryApp(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "music-playing.txt")
	for i, obs := range snaps {
		for _, got := range cfg.Evaluate(obs, testThreshold, time.Now()) {
			if got.State != model.StateInactive {
				t.Errorf("snapshot %d: %s state = %v, want inactive", i, got.App, got.State)
			}
			if got.Confidence != 0 {
				t.Errorf("snapshot %d: %s confidence = %v, want 0 (no call app is even running)", i, got.App, got.Confidence)
			}
		}
	}
}

// Idle desktop: nothing running that any rule cares about. Every rule stays
// inactive with zero confidence, in every snapshot.
func TestIdle_IsInactiveForEveryApp(t *testing.T) {
	cfg := liveConfig(t)
	snaps := loadFixture(t, "idle.txt")
	for i, obs := range snaps {
		for _, got := range cfg.Evaluate(obs, testThreshold, time.Now()) {
			if got.State != model.StateInactive {
				t.Errorf("snapshot %d: %s state = %v, want inactive", i, got.App, got.State)
			}
			if got.Confidence != 0 {
				t.Errorf("snapshot %d: %s confidence = %v, want 0", i, got.App, got.Confidence)
			}
		}
	}
}

// Reasons must never carry a raw window title (see model.DetectionResult's
// privacy note): every reason string must be one of the app's own generic
// labels, never the captured meeting/DM/channel name.
func TestReasons_NeverContainWindowTitles(t *testing.T) {
	cfg := liveConfig(t)
	for _, fixture := range []string{
		"teams-in-call.txt", "teams-in-call-muted.txt", "teams-in-call-generic-title.txt", "teams-open-no-call.txt",
		"zoom-in-call.txt", "zoom-open-no-call.txt", "slack-huddle.txt",
		"meet-landing-page.txt", "meet-lobby.txt", "meet-in-call.txt", "meet-left.txt",
	} {
		for _, obs := range loadFixture(t, fixture) {
			for _, result := range cfg.Evaluate(obs, testThreshold, time.Now()) {
				for _, reason := range result.Reasons {
					for _, w := range obs.Windows {
						if w.Title != "" && strings.Contains(reason, w.Title) {
							t.Errorf("fixture %s: reason %q leaked window title %q", fixture, reason, w.Title)
						}
					}
				}
			}
		}
	}
}
