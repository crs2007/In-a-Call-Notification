//go:build !windows

// Package windows holds every Win32 syscall CallMQTT makes. This file is the
// non-Windows half of that contract: it exists purely so packages that
// import platform/windows unconditionally (e.g. cmd/probe) still build on
// darwin and linux. Every function here is a no-op that reports "no
// evidence" rather than failing, exactly as the real implementation does
// when a signal is genuinely absent.
package windows

import "log/slog"

// WindowInfo mirrors the Windows implementation's shape so callers compile
// unchanged on every platform.
type WindowInfo struct {
	PID   uint32
	Title string
}

// VisibleWindows is unimplemented outside Windows.
func VisibleWindows() []WindowInfo {
	slog.Debug("VisibleWindows: unsupported platform")
	return nil
}

// AppsUsingMicrophone is unimplemented outside Windows.
func AppsUsingMicrophone(map[uint32]string) []string {
	slog.Debug("AppsUsingMicrophone: unsupported platform")
	return nil
}

// AppsUsingWebcam is unimplemented outside Windows.
func AppsUsingWebcam(map[uint32]string) []string {
	slog.Debug("AppsUsingWebcam: unsupported platform")
	return nil
}

// ProcessNames is unimplemented outside Windows.
func ProcessNames() map[uint32]string {
	slog.Debug("ProcessNames: unsupported platform")
	return nil
}
