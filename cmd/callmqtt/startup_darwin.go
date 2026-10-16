//go:build darwin

package main

import (
	platformdarwin "github.com/crs2007/callmqtt/platform/darwin"
)

// macStartup adapts platform/darwin's LaunchAgent functions to the shape the
// tray and the CLI subcommand want, the same shape winStartup has.
//
// Loading into launchd now is right only for the CLI. RunAtLoad makes a
// bootstrap start an instance immediately, so the tray of an agent that is
// already running would start a second copy; and a bootout from inside the
// login-launched copy would stop the very process asking. A running agent
// therefore only writes or removes the plist, which launchd reads at the
// next login anyway.
type macStartup struct{}

func (macStartup) IsEnabled() (bool, error) { return platformdarwin.StartupEnabled() }
func (macStartup) Enable() error {
	return platformdarwin.EnableStartup(startupConfigPath, !agentRunning)
}
func (macStartup) Disable() error { return platformdarwin.DisableStartup(!agentRunning) }

// Status reports a plist that launches a different executable than the
// running one, e.g. after the app was moved out of Downloads.
func (macStartup) Status() (enabled, stale bool, target string, err error) {
	return platformdarwin.StartupStatus()
}

var startup = macStartup{}

// startupSupported tells ui_tray.go whether to offer the login checkbox.
const startupSupported = true
