//go:build !windows && !darwin

package main

import "errors"

// errStartupUnsupported is returned by every stubStartup method on platforms
// where autostart isn't implemented yet.
// startupSupported tells ui_tray.go whether to offer the login checkbox.
const startupSupported = false

var errStartupUnsupported = errors.New("start-at-login is not supported on this platform")

// stubStartup has no implementation outside Windows and macOS yet. The tray
// omits the "Start at login" checkbox (startupSupported is false, so
// ui_tray.go passes it a nil Startup), and the CLI subcommand reports this
// error to the user.
type stubStartup struct{}

func (stubStartup) IsEnabled() (bool, error) { return false, errStartupUnsupported }
func (stubStartup) Enable() error            { return errStartupUnsupported }
func (stubStartup) Disable() error           { return errStartupUnsupported }

func (stubStartup) Status() (bool, bool, string, error) {
	return false, false, "", errStartupUnsupported
}

var startup = stubStartup{}
