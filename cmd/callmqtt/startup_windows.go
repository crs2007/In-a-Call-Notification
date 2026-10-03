//go:build windows

package main

import (
	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

// winStartup adapts platform/windows's three free functions to the shape the
// tray (and the CLI subcommand) want. Declared here, not tray-tagged, so a
// headless Windows build still compiles it as unused dead code, and
// non-Windows builds get startup_stub.go instead.
type winStartup struct{}

func (winStartup) IsEnabled() (bool, error) { return platformwindows.StartupEnabled() }
func (winStartup) Enable() error            { return platformwindows.EnableStartup(startupConfigPath) }

// Status reports a run value that points at a different executable than the
// running one, e.g. after the extracted folder was moved.
func (winStartup) Status() (enabled, stale bool, target string, err error) {
	return platformwindows.StartupStatus()
}
func (winStartup) Disable() error { return platformwindows.DisableStartup() }

var startup = winStartup{}
