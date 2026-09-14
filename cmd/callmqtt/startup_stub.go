//go:build !windows

package main

import "errors"

// errStartupUnsupported is returned by every stubStartup method on platforms
// where autostart isn't implemented yet.
var errStartupUnsupported = errors.New("start-at-login is not supported on this platform")

// stubStartup has no implementation outside Windows yet; the tray falls back
// to omitting the "Start at login" checkbox (see internal/tray's nil-Startup
// handling), and the CLI subcommand reports the same error to the user.
type stubStartup struct{}

func (stubStartup) IsEnabled() (bool, error) { return false, errStartupUnsupported }
func (stubStartup) Enable() error            { return errStartupUnsupported }
func (stubStartup) Disable() error           { return errStartupUnsupported }

var startup = stubStartup{}
