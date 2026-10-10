//go:build tray && !darwin

package main

import "github.com/crs2007/callmqtt/internal/tray"

// trayPermission has nothing to offer outside macOS: Windows reads window
// titles and microphone use without asking.
func trayPermission() *tray.Permission { return nil }
