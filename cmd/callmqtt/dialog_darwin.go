//go:build darwin

package main

import (
	"github.com/crs2007/callmqtt/internal/config"
	platformdarwin "github.com/crs2007/callmqtt/platform/darwin"
)

// reportError shows a startup failure in an alert. main calls it when no
// terminal is attached (see consoleAttached): opened from Finder or started
// at login, the app's stderr is /dev/null, so the write main already made
// succeeds and is seen by nobody — without this, a first-run error (no
// config yet) just makes the app exit with nothing on screen.
func reportError(err error) {
	platformdarwin.ShowError(appTitle, err.Error())
}

// reportInfo is reportError for a message that is not a failure — `callmqtt
// init` saying where the starter config went — under the same rule.
func reportInfo(message string) {
	platformdarwin.ShowInfo(appTitle, message)
}

// brokerDialog is nil on macOS: there is no native broker form yet, and a
// nil tray.Dialog makes "MQTT broker settings…" open config.yaml instead. A
// function that always returned "cancelled" would leave that item doing
// nothing at all.
var brokerDialog func(config.Settings) (config.Settings, bool, error)
