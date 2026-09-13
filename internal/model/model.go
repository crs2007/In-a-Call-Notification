// Package model defines the vocabulary shared by every layer of CallMQTT:
// what a detector observes, and what "in a call" resolves to.
//
// It deliberately depends on nothing. Everything else in the project may
// import it; it imports no other package in the project.
package model

import (
	"context"
	"time"
)

// CallState is the answer to the only question CallMQTT asks.
type CallState string

const (
	// StateUnknown means no detector could form an opinion — typically at
	// startup, or when every signal source failed. It is never published as
	// an active state: when in doubt, the light stays off.
	StateUnknown CallState = "unknown"

	// StateInactive means the user is demonstrably not in a call.
	StateInactive CallState = "inactive"

	// StateActive means the user is in a call.
	StateActive CallState = "active"
)

// Signal is one piece of weak evidence contributing to a confidence score,
// such as "the Teams process is running" or "something holds the microphone".
// No single signal is ever sufficient on its own.
type Signal struct {
	Name   string
	Weight float64
}

// DetectionResult is one detector's opinion about one application.
//
// Reasons carries the human-readable signal names that produced Confidence,
// so `callmqtt diagnose` can explain a verdict without re-running detection.
// It must never contain a window title: titles carry meeting names, and this
// struct is one step away from being serialised.
type DetectionResult struct {
	App        string
	State      CallState
	Confidence float64
	Reasons    []string
	Timestamp  time.Time
}

// Detector reports whether one application is currently in a call.
//
// Detect never returns an error. A signal source that fails contributes no
// confidence and is noted in Reasons; the agent keeps running with degraded
// detection rather than going dark.
type Detector interface {
	App() string
	Detect(ctx context.Context) DetectionResult
}

// ClampConfidence constrains a summed confidence score to [0, 1].
func ClampConfidence(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
