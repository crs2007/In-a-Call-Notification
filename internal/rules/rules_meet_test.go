package rules

import (
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
)

// -----------------------------------------------------------------------
// Google Meet: synthetic scenarios.
//
// No testdata/probe/meet-*.txt capture exists yet (see the scenario table in
// testdata/probe/README.md), so these observations are built inline. The
// browser-window shape is copied from real chrome.exe windows in the
// existing captures - "\u202a<tab title> - Google Chrome\u202c" in
// teams-in-call.txt - with only the tab title replaced by Meet's. Once the
// meet-* fixtures are captured these should become loadFixture-driven, like
// every other app's tests.
// -----------------------------------------------------------------------

// chromeMicEntry is the shape of a NonPackaged ConsentStore entry for
// Chrome, matching the Zoom.exe path entry in zoom-in-call.txt.
const chromeMicEntry = `C:\Program Files\Google\Chrome\Application\chrome.exe`

// defaultInactiveThreshold mirrors config.Defaults().Detection.InactiveThreshold;
// the meet rule's weights are tuned against it (see rules.yaml).
const defaultInactiveThreshold = 0.30

// meetWindows returns the visible windows of a browser session with the
// Meet tab in front of one window plus the unrelated tabs/windows seen in
// the real captures.
func meetWindows(meetTitle string) []WindowObservation {
	return []WindowObservation{
		{Proc: "chrome.exe", Title: "\u202aGoogle Translate - Google Chrome\u202c"},
		{Proc: "chrome.exe", Title: meetTitle},
		{Proc: "brave.exe", Title: "Redirecting… | Slack - Brave"},
		{Proc: "Spotify.exe", Title: "Spotify Premium"},
	}
}

func evalMeet(t *testing.T, obs Observation) model.DetectionResult {
	t.Helper()
	return find(t, liveConfig(t).Evaluate(obs, testThreshold, time.Now()), "meet")
}

// Joined, Meet tab in front: title + mic + playback -> active.
func TestMeetInCall_IsActive(t *testing.T) {
	for _, title := range []string{
		"\u202aMeet – abc-defg-hij - Google Chrome\u202c", // meeting code, en dash
		"Meet – Weekly sync - Brave",                      // calendar event name
		"Meet - abc-defg-hij - Microsoft Edge",            // hyphen separator
		"Meet – abc-defg-hij",                             // installed-app (PWA) window
		"(1) Meet – abc-defg-hij - Google Chrome",         // tab badge prefix
	} {
		obs := Observation{
			Windows:       meetWindows(title),
			MicInUse:      []string{chromeMicEntry},
			CamInUse:      []string{chromeMicEntry},
			AudioOutInUse: []string{"chrome.exe"},
		}
		got := evalMeet(t, obs)
		if got.State != model.StateActive {
			t.Errorf("title %q: meet state = %v, confidence = %v, want active", title, got.State, got.Confidence)
		}
	}
}

// The "Ready to join?" lobby: same title, mic and camera held for the
// self-view preview, but no playback stream. Must never cross the active
// threshold - this is the whole reason the rule needs three signals.
func TestMeetLobby_IsInactive(t *testing.T) {
	obs := Observation{
		Windows:  meetWindows("\u202aMeet – abc-defg-hij - Google Chrome\u202c"),
		MicInUse: []string{chromeMicEntry},
		CamInUse: []string{chromeMicEntry},
	}
	got := evalMeet(t, obs)
	if got.State != model.StateInactive {
		t.Errorf("lobby: meet state = %v, confidence = %v, want inactive", got.State, got.Confidence)
	}
	if got.Confidence >= testThreshold {
		t.Errorf("lobby: meet confidence = %v crossed the active threshold without a playback stream", got.Confidence)
	}
}

// Mid-call, the user switches to another tab in the same window: the Meet
// title disappears (a browser only shows its active tab's title) but the
// browser still holds the mic and renders call audio. That must stay at or
// above the inactive threshold so the detector's hysteresis keeps the call
// active, yet below the active threshold so the same evidence can never
// *start* a call (a YouTube tab plus a Discord web call looks identical).
func TestMeetTabSwitchedAway_HoldsButNeverStarts(t *testing.T) {
	obs := Observation{
		Windows: []WindowObservation{
			{Proc: "chrome.exe", Title: "\u202aGoogle Translate - Google Chrome\u202c"},
		},
		MicInUse:      []string{chromeMicEntry},
		AudioOutInUse: []string{"chrome.exe"},
	}
	got := evalMeet(t, obs)
	if got.Confidence < defaultInactiveThreshold {
		t.Errorf("tab switched away: meet confidence = %v fell below the inactive threshold; a call would drop as soon as the user looks at another tab", got.Confidence)
	}
	if got.State != model.StateInactive || got.Confidence >= testThreshold {
		t.Errorf("tab switched away: meet state = %v, confidence = %v; mic + playback without a Meet title must not start a call", got.State, got.Confidence)
	}
}

// Every single signal on its own must stay below the inactive threshold: a
// lingering "Meet – ..." title after leaving, a bare mic grant, or a YouTube
// tab must not keep a finished call alive through hysteresis, let alone
// start one.
func TestMeetSingleSignal_IsBelowInactiveThreshold(t *testing.T) {
	cases := map[string]Observation{
		"title only (left the call)": {Windows: meetWindows("\u202aMeet – abc-defg-hij - Google Chrome\u202c")},
		"mic only":                   {Windows: meetWindows("\u202aGoogle Translate - Google Chrome\u202c"), MicInUse: []string{chromeMicEntry}},
		"playback only (YouTube)":    {Windows: meetWindows("\u202aGoogle Translate - Google Chrome\u202c"), AudioOutInUse: []string{"chrome.exe"}},
	}
	for name, obs := range cases {
		got := evalMeet(t, obs)
		if got.State != model.StateInactive {
			t.Errorf("%s: meet state = %v, want inactive", name, got.State)
		}
		if got.Confidence >= defaultInactiveThreshold {
			t.Errorf("%s: meet confidence = %v reached the inactive threshold on a single signal", name, got.Confidence)
		}
	}
}

// Titles that merely mention Meet must not count as a Meet meeting window:
// Teams' own "Meet" utility window (wrong process *and* wrong shape), the
// Meet landing page, and prose that happens to contain the word.
func TestMeetTitle_RejectsLookalikes(t *testing.T) {
	for _, w := range []WindowObservation{
		{Proc: "ms-teams.exe", Title: "Meet | Microsoft Teams"},
		{Proc: "chrome.exe", Title: "\u202aGoogle Meet - Google Chrome\u202c"},
		{Proc: "chrome.exe", Title: "\u202aHow to Meet – a guide - Google Chrome\u202c"},
		{Proc: "chrome.exe", Title: "\u202aMeet – - Google Chrome\u202c"},
	} {
		obs := Observation{
			Windows:       []WindowObservation{w},
			MicInUse:      []string{chromeMicEntry},
			AudioOutInUse: []string{"chrome.exe"},
		}
		got := evalMeet(t, obs)
		if got.State != model.StateInactive || got.Confidence >= testThreshold {
			t.Errorf("title %q: meet state = %v, confidence = %v; title must not have matched", w.Title, got.State, got.Confidence)
		}
	}
}

// A browser is always open, so "process present" must be worth nothing:
// browser windows alone contribute 0 to meet. (The idle and music-playing
// fixtures already assert confidence 0 for every app; this pins the reason.)
func TestMeetProcessPresence_IsWorthless(t *testing.T) {
	obs := Observation{Windows: meetWindows("\u202aGoogle Translate - Google Chrome\u202c")}
	got := evalMeet(t, obs)
	if got.Confidence != 0 {
		t.Errorf("meet confidence = %v with only browser windows open, want 0", got.Confidence)
	}
}

// Playback attribution is by bare exe name (platform/windows/audio.go), and
// a music player's stream must never be read as the browser's.
func TestMeetAudioOut_OnlyBrowserProcessesCount(t *testing.T) {
	obs := Observation{
		Windows:       meetWindows("\u202aMeet – abc-defg-hij - Google Chrome\u202c"),
		MicInUse:      []string{chromeMicEntry},
		AudioOutInUse: []string{"Spotify.exe", "pwsh.exe"},
	}
	got := evalMeet(t, obs)
	if got.State != model.StateInactive || got.Confidence >= testThreshold {
		t.Errorf("meet state = %v, confidence = %v; Spotify's playback must not count as the browser's", got.State, got.Confidence)
	}
}
