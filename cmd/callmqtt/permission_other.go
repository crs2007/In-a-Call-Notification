//go:build !darwin

package main

import "log/slog"

// offerScreenRecording is macOS-only; elsewhere nothing needs granting.
func offerScreenRecording(*slog.Logger, string) {}
