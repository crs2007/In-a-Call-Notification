package detection

import (
	"time"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/model"
)

// Machine holds a candidate state until it has persisted long enough to be
// believed, so that momentary detector noise never reaches the light.
//
// The two debounces are deliberately different. Entering a call should feel
// immediate, so the enter debounce is short. Leaving is held longer, because
// muting, toggling a camera or a brief network reconnect can all make a call
// look momentarily absent, and a light that blinks off mid-call is worse than
// one that lingers a few seconds after it ends.
//
// Machine takes the current time as a parameter and never reads the clock
// itself. That is what makes every timing rule here testable in microseconds.
type Machine struct {
	enterDebounce time.Duration
	exitDebounce  time.Duration

	effective model.CallState // what the world has been told
	candidate model.CallState // what detectors are saying now
	since     time.Time       // when the candidate first appeared
	started   bool
}

// NewMachine returns a Machine in the unknown state.
func NewMachine(cfg config.Detection) *Machine {
	return &Machine{
		enterDebounce: cfg.EnterDebounce(),
		exitDebounce:  cfg.ExitDebounce(),
		effective:     model.StateUnknown,
		candidate:     model.StateUnknown,
	}
}

// State returns the current effective state without advancing the machine.
func (m *Machine) State() model.CallState { return m.effective }

// Update feeds one resolution into the machine and reports the effective state,
// along with whether it just changed. Only a change is worth publishing.
func (m *Machine) Update(r Resolved, now time.Time) (model.CallState, bool) {
	if !m.started {
		m.started = true
		m.candidate = r.State
		m.since = now
	}

	if r.State != m.candidate {
		m.candidate = r.State
		m.since = now
	}

	if m.candidate == m.effective {
		return m.effective, false
	}

	if now.Sub(m.since) < m.debounceFor(m.candidate) {
		return m.effective, false
	}

	m.effective = m.candidate
	return m.effective, true
}

// debounceFor returns how long the given candidate must persist before it is
// believed. Anything that is not a confirmed call uses the exit debounce:
// losing the signal entirely is a reason to turn the light off, not a reason
// to leave it on.
func (m *Machine) debounceFor(candidate model.CallState) time.Duration {
	if candidate == model.StateActive {
		return m.enterDebounce
	}
	return m.exitDebounce
}
