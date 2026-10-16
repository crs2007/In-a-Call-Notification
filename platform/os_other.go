//go:build !windows && !darwin

package platform

import "log/slog"

// VisibleWindows is unimplemented on this OS.
func VisibleWindows() []WindowInfo {
	slog.Debug("VisibleWindows: unsupported platform")
	return nil
}

// ProcessNames is unimplemented on this OS.
func ProcessNames() map[uint32]string {
	slog.Debug("ProcessNames: unsupported platform")
	return nil
}

// AppsUsingMicrophone is unimplemented on this OS.
func AppsUsingMicrophone(map[uint32]string) []string {
	slog.Debug("AppsUsingMicrophone: unsupported platform")
	return nil
}

// AppsUsingWebcam is unimplemented on this OS.
func AppsUsingWebcam(map[uint32]string) []string {
	slog.Debug("AppsUsingWebcam: unsupported platform")
	return nil
}
