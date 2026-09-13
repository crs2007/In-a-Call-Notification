package detection

import (
	"reflect"
	"testing"

	"github.com/crs2007/callmqtt/internal/model"
)

func result(app string, state model.CallState, confidence float64) model.DetectionResult {
	return model.DetectionResult{App: app, State: state, Confidence: confidence}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name           string
		results        []model.DetectionResult
		wantState      model.CallState
		wantApp        string
		wantApps       []string
		wantConfidence float64
	}{
		{
			name:      "no detectors at all",
			results:   nil,
			wantState: model.StateUnknown,
		},
		{
			name: "nobody is in a call",
			results: []model.DetectionResult{
				result("teams", model.StateInactive, 0.20),
				result("zoom", model.StateInactive, 0.00),
			},
			wantState:      model.StateInactive,
			wantConfidence: 0.20,
		},
		{
			name: "one active app",
			results: []model.DetectionResult{
				result("teams", model.StateActive, 0.85),
				result("zoom", model.StateInactive, 0.20),
				result("slack", model.StateInactive, 0.00),
			},
			wantState:      model.StateActive,
			wantApp:        "teams",
			wantApps:       []string{"teams"},
			wantConfidence: 0.85,
		},
		{
			name: "two active apps are both reported, most confident names the state",
			results: []model.DetectionResult{
				result("zoom", model.StateActive, 0.75),
				result("teams", model.StateActive, 0.95),
			},
			wantState:      model.StateActive,
			wantApp:        "teams",
			wantApps:       []string{"teams", "zoom"},
			wantConfidence: 0.95,
		},
		{
			name: "equal confidence breaks ties alphabetically, not by order",
			results: []model.DetectionResult{
				result("zoom", model.StateActive, 0.80),
				result("slack", model.StateActive, 0.80),
			},
			wantState:      model.StateActive,
			wantApp:        "slack",
			wantApps:       []string{"slack", "zoom"},
			wantConfidence: 0.80,
		},
		{
			name: "every detector blind stays unknown",
			results: []model.DetectionResult{
				result("teams", model.StateUnknown, 0),
				result("zoom", model.StateUnknown, 0),
			},
			wantState: model.StateUnknown,
		},
		{
			name: "one blind detector does not mask a confident one",
			results: []model.DetectionResult{
				result("teams", model.StateUnknown, 0),
				result("zoom", model.StateActive, 0.90),
			},
			wantState:      model.StateActive,
			wantApp:        "zoom",
			wantApps:       []string{"zoom"},
			wantConfidence: 0.90,
		},
		{
			name: "a blind detector alongside an inactive one is inactive, never active",
			results: []model.DetectionResult{
				result("teams", model.StateUnknown, 0),
				result("zoom", model.StateInactive, 0.20),
			},
			wantState:      model.StateInactive,
			wantConfidence: 0.20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.results)

			if got.State != tt.wantState {
				t.Errorf("state = %q, want %q", got.State, tt.wantState)
			}
			if got.App != tt.wantApp {
				t.Errorf("app = %q, want %q", got.App, tt.wantApp)
			}
			if !reflect.DeepEqual(got.Apps, tt.wantApps) {
				t.Errorf("apps = %v, want %v", got.Apps, tt.wantApps)
			}
			if got.Confidence != tt.wantConfidence {
				t.Errorf("confidence = %v, want %v", got.Confidence, tt.wantConfidence)
			}
		})
	}
}

// Detector registration order and map iteration must never change the answer.
func TestResolveIsOrderIndependent(t *testing.T) {
	forward := Resolve([]model.DetectionResult{
		result("teams", model.StateActive, 0.90),
		result("zoom", model.StateActive, 0.90),
		result("slack", model.StateInactive, 0.20),
	})
	reverse := Resolve([]model.DetectionResult{
		result("slack", model.StateInactive, 0.20),
		result("zoom", model.StateActive, 0.90),
		result("teams", model.StateActive, 0.90),
	})

	if !reflect.DeepEqual(forward, reverse) {
		t.Errorf("resolution depends on input order:\n forward = %+v\n reverse = %+v", forward, reverse)
	}
}
