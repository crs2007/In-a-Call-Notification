package rules

import (
	"strings"
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
)

// -----------------------------------------------------------------------
// Google Meet: fixture-driven scenarios, captured live in Chrome on
// 2026-09-18 (see rules.yaml for the per-scenario titles and mic entries).
// -----------------------------------------------------------------------

// defaultInactiveThreshold mirrors config.Defaults().Detection.InactiveThreshold;
// the meet rule's weights are tuned against it (see rules.yaml).
const defaultInactiveThreshold = 0.30

func evalMeet(t *testing.T, obs Observation) model.DetectionResult {
	t.Helper()
	return find(t, liveConfig(t).Evaluate(obs, testThreshold, time.Now()), "meet")
}

// hasMeetTitle reports whether a snapshot contains the captured Meet
// meeting window ("Meet - <code>", with or without the browser suffix).
func hasMeetTitle(obs Observation) bool {
	for _, w := range obs.Windows {
		if w.Proc == "chrome.exe" && strings.Contains(w.Title, "Meet - abc-defg-hij") {
			return true
		}
	}
	return false
}

// hasChromeMic reports whether Chrome holds the microphone in a snapshot.
func hasChromeMic(obs Observation) bool {
	for _, m := range obs.MicInUse {
		if strings.HasSuffix(strings.ToLower(m), `\chrome.exe`) {
			return true
		}
	}
	return false
}

// The Meet landing page ("Google Meet - Google Chrome") with the browser
// otherwise idle is worth exactly nothing: not just inactive, but zero
// confidence, because a browser being open is no evidence at all.
func TestMeetLandingPage_IsZero(t *testing.T) {
	for i, obs := range loadFixture(t, "meet-landing-page.txt") {
		got := evalMeet(t, obs)
		if got.State != model.StateInactive || got.Confidence != 0 {
			t.Errorf("snapshot %d: meet state = %v, confidence = %v, want inactive/0", i, got.State, got.Confidence)
		}
	}
}

// The lobby capture walks from the landing page (0), through the bare
// "Meet - Google Chrome" transition page (snapshot 32: title does not
// match, so still 0) to "Ready to join?" (title + mic = 0.75). The lobby is
// deliberately active - see rules.yaml for why it cannot be told apart from
// the call - and this test pins that decision so a future "fix" that makes
// the lobby inactive is forced to explain how it tells the two apart.
func TestMeetLobby_IsActiveOnceMicHeld(t *testing.T) {
	snaps := loadFixture(t, "meet-lobby.txt")
	lobbySeen := 0
	for i, obs := range snaps {
		got := evalMeet(t, obs)
		switch {
		case hasMeetTitle(obs) && hasChromeMic(obs):
			lobbySeen++
			if got.State != model.StateActive {
				t.Errorf("snapshot %d (lobby): meet state = %v, confidence = %v, want active", i, got.State, got.Confidence)
			}
		default:
			if got.State != model.StateInactive || got.Confidence >= defaultInactiveThreshold {
				t.Errorf("snapshot %d (before the lobby): meet state = %v, confidence = %v, want inactive below the floor", i, got.State, got.Confidence)
			}
		}
	}
	if lobbySeen == 0 {
		t.Fatal("fixture never reached the lobby (no snapshot with Meet title + Chrome mic)")
	}
}

// Joined call: every snapshot active, including the ones where the window
// title lost its " - Google Chrome" suffix (snapshots 59-60).
func TestMeetInCall_IsActive(t *testing.T) {
	for i, obs := range loadFixture(t, "meet-in-call.txt") {
		got := evalMeet(t, obs)
		if got.State != model.StateActive {
			t.Errorf("snapshot %d: meet state = %v, confidence = %v, want active", i, got.State, got.Confidence)
		}
	}
}

// Leaving: the tab (and its "Meet - <code>" title) lingers, but Chrome
// releases the mic ~10 s after Leave. From that point the score must be
// below the inactive floor, so the detector's hysteresis lets go and the
// light goes out on mic release rather than on tab close.
func TestMeetLeft_DropsBelowFloorOnMicRelease(t *testing.T) {
	snaps := loadFixture(t, "meet-left.txt")
	released := 0
	for i, obs := range snaps {
		got := evalMeet(t, obs)
		if !hasMeetTitle(obs) {
			t.Fatalf("snapshot %d: expected the lingering Meet tab title in every post-call snapshot", i)
		}
		if hasChromeMic(obs) {
			continue // still winding down, indistinguishable from the call
		}
		released++
		if got.State != model.StateInactive || got.Confidence >= defaultInactiveThreshold {
			t.Errorf("snapshot %d (mic released): meet state = %v, confidence = %v, want inactive below the floor", i, got.State, got.Confidence)
		}
	}
	if released == 0 {
		t.Fatal("fixture never shows Chrome releasing the mic")
	}
}

// Mid-call, the user switches to another tab in the same window: the Meet
// title disappears (a browser only shows its active tab's title) but the
// browser still holds the mic. That must stay at or above the inactive
// floor so hysteresis keeps the call active, yet below the active threshold
// so a bare mic grant (voice typing, a Discord web call) can never *start*
// one. Synthetic: no capture switched tabs mid-call yet.
func TestMeetTabSwitchedAway_HoldsButNeverStarts(t *testing.T) {
	obs := Observation{
		Windows: []WindowObservation{
			{Proc: "chrome.exe", Title: "\u202aGoogle Translate - Google Chrome\u202c"},
		},
		MicInUse: []string{`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`},
	}
	got := evalMeet(t, obs)
	if got.Confidence < defaultInactiveThreshold {
		t.Errorf("mic only: meet confidence = %v fell below the inactive floor; a call would drop as soon as the user looks at another tab", got.Confidence)
	}
	if got.State != model.StateInactive || got.Confidence >= testThreshold {
		t.Errorf("mic only: meet state = %v, confidence = %v; a bare mic grant must not start a call", got.State, got.Confidence)
	}
}

// Title shapes not yet captured but allowed by the include regex: other
// browsers' suffixes, en/em dash separators, a calendar event name as the
// subject, and a "(1) " tab badge. Each with the mic held must be active.
func TestMeetTitle_AcceptsDocumentedVariants(t *testing.T) {
	for _, w := range []WindowObservation{
		{Proc: "msedge.exe", Title: "Meet - abc-defg-hij - Microsoft Edge"},
		{Proc: "brave.exe", Title: "Meet – Weekly sync - Brave"},
		{Proc: "firefox.exe", Title: "Meet — abc-defg-hij — Mozilla Firefox"},
		{Proc: "chrome.exe", Title: "(1) Meet - abc-defg-hij - Google Chrome"},
	} {
		obs := Observation{
			Windows:  []WindowObservation{w},
			MicInUse: []string{`C:\Program Files\` + strings.TrimSuffix(w.Proc, ".exe") + `\` + w.Proc},
		}
		got := evalMeet(t, obs)
		if got.State != model.StateActive {
			t.Errorf("title %q (%s): meet state = %v, confidence = %v, want active", w.Title, w.Proc, got.State, got.Confidence)
		}
	}
}

// Titles that merely mention Meet must not count: Teams' own "Meet" utility
// window (wrong process and wrong shape), the landing page, the bare
// transition page, prose containing the word, and an empty subject.
func TestMeetTitle_RejectsLookalikes(t *testing.T) {
	for _, w := range []WindowObservation{
		{Proc: "ms-teams.exe", Title: "Meet | Microsoft Teams"},
		{Proc: "chrome.exe", Title: "\u202aGoogle Meet - Google Chrome\u202c"},
		{Proc: "chrome.exe", Title: "\u202aMeet - Google Chrome\u202c"},
		{Proc: "chrome.exe", Title: "\u202aHow to Meet – a guide - Google Chrome\u202c"},
		{Proc: "chrome.exe", Title: "\u202aMeet – - Google Chrome\u202c"},
	} {
		obs := Observation{
			Windows:  []WindowObservation{w},
			MicInUse: []string{`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`},
		}
		got := evalMeet(t, obs)
		if got.State != model.StateInactive || got.Confidence >= testThreshold {
			t.Errorf("title %q: meet state = %v, confidence = %v; title must not have matched", w.Title, got.State, got.Confidence)
		}
	}
}
