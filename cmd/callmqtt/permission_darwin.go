//go:build darwin

package main

import (
	"log/slog"
	"os"
	"path/filepath"

	platformdarwin "github.com/crs2007/callmqtt/platform/darwin"
)

// screenRecordingAskedFile marks, next to config.yaml, that the first-launch
// explainer has already been shown, so a user who said no is not asked again
// on every login. The tray item stays available for changing their mind.
const screenRecordingAskedFile = "screen-recording-asked"

const screenRecordingExplainer = `In a Call Notification reads the titles of meeting windows (for example "Zoom Meeting") to tell a call apart from an app that is merely open. macOS counts reading other apps' window titles as Screen Recording.

Nothing is recorded or captured, and no image of your screen is ever taken.

Next, macOS will ask for the permission. To grant it later, use System Settings → Privacy & Security → Screen & System Audio Recording, or the menu-bar item "Allow window titles (Screen Recording)…", then quit and reopen the app.

Without it, calls are still detected from the running app and its microphone use, just less precisely.`

// offerScreenRecording shows the explainer and the system prompt once, on
// the first launch that has no terminal attached (opened from Finder or at
// login) and no permission yet. It never blocks the agent: the alert runs on
// its own goroutine while detection starts with whatever evidence it has.
func offerScreenRecording(log *slog.Logger, configPath string) {
	if consoleAttached() || platformdarwin.ScreenRecordingGranted() {
		return
	}
	marker := filepath.Join(filepath.Dir(configPath), screenRecordingAskedFile)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		// Asking again next launch is better than never asking.
		log.Debug("record screen recording prompt", "error", err)
	}
	go func() {
		platformdarwin.ShowInfo(appTitle, screenRecordingExplainer)
		platformdarwin.RequestScreenRecording()
	}()
}
