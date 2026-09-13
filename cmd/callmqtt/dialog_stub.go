//go:build !windows

package main

import "github.com/crs2007/callmqtt/internal/config"

// brokerDialog has no implementation outside Windows yet; the tray falls
// back to opening the config file directly (see internal/tray's nil-Dialog
// handling).
func brokerDialog(config.Settings) (config.Settings, bool, error) {
	return config.Settings{}, false, nil
}
