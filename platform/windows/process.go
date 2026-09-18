//go:build windows

package windows

import (
	"log/slog"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessNames maps every running process's PID to its executable name.
// Callers pair this with VisibleWindows' PIDs to label window titles.
//
// A process that disappears mid-enumeration, or whose name can't be read
// (e.g. a protected system process), is skipped rather than treated as a
// failure. Likewise, a failure to create the snapshot at all is not a fatal
// error: it means "no evidence" for this poll cycle, so this logs at
// slog.Debug and returns an empty (non-nil) map, letting the caller carry on
// with degraded detection rather than die.
func ProcessNames() map[uint32]string {
	names := make(map[uint32]string)

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		slog.Debug("create process snapshot failed", "err", err)
		return names
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snapshot, &entry); err != nil {
		slog.Debug("enumerate processes failed", "err", err)
		return names
	}
	for {
		names[entry.ProcessID] = exeFileToString(entry.ExeFile)

		entry.Size = uint32(unsafe.Sizeof(entry))
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break // ERROR_NO_MORE_FILES once the walk is exhausted
		}
	}
	return names
}

// exeFileToString converts a ProcessEntry32.ExeFile fixed-size buffer to a
// Go string, trimming at the first NUL.
func exeFileToString(exeFile [windows.MAX_PATH]uint16) string {
	for i, c := range exeFile {
		if c == 0 {
			return windows.UTF16ToString(exeFile[:i])
		}
	}
	return windows.UTF16ToString(exeFile[:])
}
