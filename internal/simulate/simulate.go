// Package simulate provides a detector that fakes a call on a fixed cycle.
//
// It exists so the whole chain — state machine, network gate, broker, Home
// Assistant entity, automation, light — can be proven end to end without
// anyone having to join a real meeting. It is also the fastest way for a user
// to check that their automation turns the right light the right colour.
package simulate

import (
	"context"
	"time"

	"github.com/crs2007/callmqtt/internal/model"
)

// Cycle is how long the simulator spends in each state.
const Cycle = 30 * time.Second

// Detector alternates between active and inactive.
type Detector struct {
	start time.Time
	now   func() time.Time
}

// NewDetector returns a simulated detector that starts inactive.
func NewDetector() *Detector {
	return &Detector{start: time.Now(), now: time.Now}
}

// App implements model.Detector.
func (d *Detector) App() string { return "simulated" }

// Detect reports active for one cycle, then inactive for the next.
func (d *Detector) Detect(context.Context) model.DetectionResult {
	now := d.now()
	elapsed := now.Sub(d.start)
	active := (elapsed/Cycle)%2 == 1

	result := model.DetectionResult{
		App:       "simulated",
		State:     model.StateInactive,
		Reasons:   []string{"simulated: idle"},
		Timestamp: now,
	}
	if active {
		result.State = model.StateActive
		result.Confidence = 1.0
		result.Reasons = []string{"simulated: in a call"}
	}
	return result
}
