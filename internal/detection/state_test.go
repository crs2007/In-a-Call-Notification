package detection

import (
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/model"
)

// testDetection uses the shipped defaults: 2s to believe a call started,
// 8s to believe it ended.
func testDetection() config.Detection {
	return config.Detection{EnterDebounceSeconds: 2, ExitDebounceSeconds: 8}
}

func resolved(state model.CallState) Resolved {
	return Resolved{State: state, App: "teams"}
}

// step is one poll tick: the state detectors reported, and how far the clock
// has advanced since the machine was created.
type step struct {
	at          time.Duration
	input       model.CallState
	wantState   model.CallState
	wantChanged bool
}

func run(t *testing.T, name string, steps []step) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		m := NewMachine(testDetection())
		start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

		for i, s := range steps {
			gotState, gotChanged := m.Update(resolved(s.input), start.Add(s.at))
			if gotState != s.wantState || gotChanged != s.wantChanged {
				t.Errorf("step %d (t+%v, input %s): got (%s, changed=%v), want (%s, changed=%v)",
					i, s.at, s.input, gotState, gotChanged, s.wantState, s.wantChanged)
			}
		}
	})
}

func TestEnteringACall(t *testing.T) {
	run(t, "sustained active is believed after the enter debounce", []step{
		{0 * time.Second, model.StateInactive, model.StateUnknown, false},
		{8 * time.Second, model.StateInactive, model.StateInactive, true},
		{10 * time.Second, model.StateActive, model.StateInactive, false},
		{11 * time.Second, model.StateActive, model.StateInactive, false},
		{12 * time.Second, model.StateActive, model.StateActive, true}, // exactly 2s
		{14 * time.Second, model.StateActive, model.StateActive, false},
	})

	run(t, "a one-second blip of active never reaches the light", []step{
		{0 * time.Second, model.StateInactive, model.StateUnknown, false},
		{8 * time.Second, model.StateInactive, model.StateInactive, true},
		{10 * time.Second, model.StateActive, model.StateInactive, false},
		{11 * time.Second, model.StateInactive, model.StateInactive, false},
		{20 * time.Second, model.StateInactive, model.StateInactive, false},
	})
}

func TestLeavingACall(t *testing.T) {
	run(t, "sustained inactive is believed after the exit debounce", []step{
		{0 * time.Second, model.StateActive, model.StateUnknown, false},
		{2 * time.Second, model.StateActive, model.StateActive, true},
		{30 * time.Second, model.StateInactive, model.StateActive, false},
		{37 * time.Second, model.StateInactive, model.StateActive, false},
		{38 * time.Second, model.StateInactive, model.StateInactive, true}, // exactly 8s
	})

	// S4: muting or toggling the camera can make a call look momentarily
	// absent. The light must not blink.
	run(t, "a brief dropout mid-call does not turn the light off", []step{
		{0 * time.Second, model.StateActive, model.StateUnknown, false},
		{2 * time.Second, model.StateActive, model.StateActive, true},
		{30 * time.Second, model.StateInactive, model.StateActive, false},
		{32 * time.Second, model.StateActive, model.StateActive, false},
		{60 * time.Second, model.StateActive, model.StateActive, false},
	})

	// The exit timer restarts on every flicker back to active, so a call that
	// keeps dropping out never ends prematurely.
	run(t, "repeated flickering restarts the exit debounce", []step{
		{0 * time.Second, model.StateActive, model.StateUnknown, false},
		{2 * time.Second, model.StateActive, model.StateActive, true},
		{10 * time.Second, model.StateInactive, model.StateActive, false},
		{16 * time.Second, model.StateActive, model.StateActive, false},
		{17 * time.Second, model.StateInactive, model.StateActive, false},
		{24 * time.Second, model.StateInactive, model.StateActive, false},
		{25 * time.Second, model.StateInactive, model.StateInactive, true},
	})
}

// Losing every signal must turn the light off, not leave it stuck on.
func TestUnknownEndsTheCall(t *testing.T) {
	run(t, "going blind mid-call releases the light after the exit debounce", []step{
		{0 * time.Second, model.StateActive, model.StateUnknown, false},
		{2 * time.Second, model.StateActive, model.StateActive, true},
		{20 * time.Second, model.StateUnknown, model.StateActive, false},
		{27 * time.Second, model.StateUnknown, model.StateActive, false},
		{28 * time.Second, model.StateUnknown, model.StateUnknown, true},
	})
}

// S9: back-to-back calls must produce two clean transitions with no missed edge.
func TestBackToBackCalls(t *testing.T) {
	run(t, "leaving one call and joining another", []step{
		{0 * time.Second, model.StateActive, model.StateUnknown, false},
		{2 * time.Second, model.StateActive, model.StateActive, true},
		{60 * time.Second, model.StateInactive, model.StateActive, false},
		{68 * time.Second, model.StateInactive, model.StateInactive, true},
		{80 * time.Second, model.StateActive, model.StateInactive, false},
		{82 * time.Second, model.StateActive, model.StateActive, true},
	})
}

// A change is reported exactly once, so the engine publishes once per edge.
func TestChangeIsReportedOnlyOnce(t *testing.T) {
	m := NewMachine(testDetection())
	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

	changes := 0
	for i := 0; i < 60; i++ {
		if _, changed := m.Update(resolved(model.StateActive), start.Add(time.Duration(i)*time.Second)); changed {
			changes++
		}
	}

	if changes != 1 {
		t.Errorf("a call that stays active produced %d changes, want exactly 1", changes)
	}
	if m.State() != model.StateActive {
		t.Errorf("final state = %q, want active", m.State())
	}
}

// With no debounce configured, every reading takes effect immediately.
func TestZeroDebounceIsImmediate(t *testing.T) {
	m := NewMachine(config.Detection{})
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

	if state, changed := m.Update(resolved(model.StateActive), now); state != model.StateActive || !changed {
		t.Errorf("got (%s, %v), want (active, true)", state, changed)
	}
	if state, changed := m.Update(resolved(model.StateInactive), now); state != model.StateInactive || !changed {
		t.Errorf("got (%s, %v), want (inactive, true)", state, changed)
	}
}
