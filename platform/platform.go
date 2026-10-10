// Package platform is the one OS-neutral door to the raw signals CallMQTT's
// detectors consume: visible windows, running processes, and which
// applications hold the microphone or camera. Each function delegates to the
// adapter for the OS this binary was built for (platform/windows,
// platform/darwin), or to a no-op that reports "no evidence" elsewhere.
//
// Like the adapters behind it, nothing here interprets what it returns:
// deciding whether a window title means "in a call" is internal/rules' job.
package platform

// WindowInfo is a single top-level window: which process owns it, and its
// title. Titles often carry meeting names, so callers must never log them
// above slog.Debug.
type WindowInfo struct {
	PID   uint32
	Title string
}
