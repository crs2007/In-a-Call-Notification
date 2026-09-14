//go:build !windows

package main

import "github.com/crs2007/callmqtt/internal/config"

// reportError has no implementation outside Windows yet: non-Windows builds
// keep a console, so the stderr message main() already prints is visible
// without an extra dialog.
func reportError(error) {}

// brokerDialog has no implementation outside Windows yet; the tray falls
// back to opening the config file directly (see internal/tray's nil-Dialog
// handling).
func brokerDialog(config.Settings) (config.Settings, bool, error) {
	return config.Settings{}, false, nil
}
