//go:build !windows && !darwin

package main

import "github.com/crs2007/callmqtt/internal/config"

// reportError has no implementation outside Windows and macOS yet: those
// builds keep a console, so the stderr message main() already prints is visible
// without an extra dialog.
func reportError(error) {}

// reportInfo likewise: stdout is visible, so the printed message suffices.
func reportInfo(string) {}

// brokerDialog is nil outside Windows and macOS, as on macOS (see
// dialog_darwin.go): a nil tray.Dialog makes "MQTT broker settings…" open
// config.yaml, where a function that always returned "cancelled" would
// leave that item doing nothing at all.
var brokerDialog func(config.Settings) (config.Settings, bool, error)
