// Package detection turns per-application detector opinions into the single
// call state the agent publishes, and holds that state steady enough to drive
// a light without flickering.
package detection

import (
	"sort"

	"github.com/crs2007/callmqtt/internal/model"
)

// Resolved is the whole machine's opinion at one instant, before debouncing.
type Resolved struct {
	State      model.CallState
	App        string   // the most confident active app, empty when not active
	Apps       []string // every active app, so two simultaneous calls are reported honestly
	Confidence float64
	Reasons    []string
}

// Resolve combines per-application results into one state.
//
// Any application reporting active makes the overall state active — being in a
// Teams meeting is not made less true by Zoom sitting idle. The most confident
// active application names the state, but every active application is listed,
// because picking one arbitrarily would misrepresent what is happening.
//
// Unknown is reserved for "no detector could form an opinion at all", and is
// never treated as evidence of a call.
func Resolve(results []model.DetectionResult) Resolved {
	if len(results) == 0 {
		return Resolved{State: model.StateUnknown}
	}

	// Deterministic ordering: most confident first, then alphabetical, so the
	// reported app never depends on detector registration order or map
	// iteration.
	ordered := make([]model.DetectionResult, len(results))
	copy(ordered, results)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Confidence != ordered[j].Confidence {
			return ordered[i].Confidence > ordered[j].Confidence
		}
		return ordered[i].App < ordered[j].App
	})

	resolved := Resolved{State: model.StateUnknown}
	known := false

	for _, r := range ordered {
		if r.State != model.StateUnknown {
			known = true
		}
		if r.State != model.StateActive {
			continue
		}

		resolved.Apps = append(resolved.Apps, r.App)
		if resolved.State != model.StateActive {
			resolved.State = model.StateActive
			resolved.App = r.App
			resolved.Confidence = r.Confidence
			resolved.Reasons = r.Reasons
		}
	}

	if resolved.State == model.StateActive {
		return resolved
	}
	if known {
		// Report the highest confidence seen, so diagnostics can show how
		// close the agent came to the active threshold.
		resolved.State = model.StateInactive
		resolved.Confidence = ordered[0].Confidence
		resolved.Reasons = ordered[0].Reasons
	}
	return resolved
}
