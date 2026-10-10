//go:build tray && darwin

package main

import (
	"log/slog"
	"os/exec"

	"github.com/crs2007/callmqtt/internal/tray"
	platformdarwin "github.com/crs2007/callmqtt/platform/darwin"
)

// trayPermission offers Screen Recording from the menu bar: without it macOS
// hands every window title back empty, and detection falls back to process
// and microphone evidence. Choosing the item shows the system prompt (macOS
// only ever shows it once per app) and opens the settings pane, since after
// the first refusal the pane is the only way to grant it.
func trayPermission() *tray.Permission {
	return &tray.Permission{
		Missing: "Allow window titles (Screen Recording)…",
		Granted: "Window titles: allowed",
		Check:   platformdarwin.ScreenRecordingGranted,
		Request: func() {
			platformdarwin.RequestScreenRecording()
			if err := exec.Command("/usr/bin/open", platformdarwin.ScreenRecordingSettingsURL).Start(); err != nil {
				slog.Debug("open Screen Recording settings", "err", err)
			}
		},
	}
}
