//go:build windows && tray

package tray

import (
	"log/slog"

	"golang.org/x/sys/windows"
)

// openFileOS hands path to Explorer's default handler via ShellExecute, the
// direct Win32 call for "open this with whatever the user has associated
// with it" — the same action `cmd /c start "" path` was shelling out to
// achieve, without spawning cmd.exe or a console flash.
//
// Best-effort, matching openFile's own contract: a failure here (bad path
// encoding, no associated handler, etc.) is not something the user needs a
// dialog for, so it only reaches the log.
func openFileOS(path string) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		slog.Debug("open file: encode path", "err", err)
		return
	}
	verbPtr, err := windows.UTF16PtrFromString("open")
	if err != nil {
		slog.Debug("open file: encode verb", "err", err)
		return
	}
	if err := windows.ShellExecute(0, verbPtr, pathPtr, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		slog.Debug("open file: ShellExecute", "err", err)
	}
}
