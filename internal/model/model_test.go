package model

import "testing"

func TestClampConfidence(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  float64
	}{
		{"below range", -0.5, 0},
		{"zero", 0, 0},
		{"mid range", 0.42, 0.42},
		{"exactly one", 1, 1},
		{"summed signals overflow", 1.35, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClampConfidence(tt.score); got != tt.want {
				t.Errorf("ClampConfidence(%v) = %v, want %v", tt.score, got, tt.want)
			}
		})
	}
}
