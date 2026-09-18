//go:build windows

package main

import (
	"github.com/crs2007/callmqtt/internal/config"
	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

// reportError shows a startup failure in a MessageBox. main calls it only
// after writing to stderr failed: the shipped binary is linked
// -H=windowsgui and, launched from Explorer, the Start menu or a login
// entry, has no std handles at all — without this, a first-run error (no
// config yet) or any other startup failure just makes the exe silently
// exit with nothing on screen.
func reportError(err error) {
	platformwindows.ShowError(appTitle, err.Error())
}

// reportInfo is reportError for a message that is not a failure — `callmqtt
// init` saying where the starter config went — under the same rule: only
// once printing it has failed.
func reportInfo(message string) {
	platformwindows.ShowInfo(appTitle, message)
}

// brokerDialog adapts the Win32 broker settings dialog to the shape the tray
// wants. Declared here (windows-tagged, not tray-tagged) so a headless
// Windows build still compiles it as unused dead code, and non-Windows
// builds get dialog_stub.go instead.
func brokerDialog(current config.Settings) (config.Settings, bool, error) {
	fields, ok, err := platformwindows.ShowBrokerDialog(platformwindows.BrokerFields{
		Host:     current.BrokerHost,
		Port:     current.BrokerPort,
		Username: current.Username,
		Password: current.Password,
	})
	if err != nil || !ok {
		return config.Settings{}, false, err
	}

	next := current
	next.BrokerHost = fields.Host
	next.BrokerPort = fields.Port
	next.Username = fields.Username
	next.Password = fields.Password
	next.PasswordChanged = fields.Password != current.Password
	return next, true, nil
}
