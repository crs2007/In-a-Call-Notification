package darwin

// WindowInfo mirrors platform.WindowInfo so platform/os_darwin.go can
// convert between the two without this package importing its caller.
// Titles often carry meeting names, so callers must never log them above
// slog.Debug.
type WindowInfo struct {
	PID   uint32
	Title string
}

// normalWindowLayer is kCGNormalWindowLevel: ordinary application windows.
// Everything else on screen — the menu bar, status items, the Dock,
// notification banners, screen savers, floating utility panels — sits on a
// higher layer and is not "the app the user is working in".
const normalWindowLayer = 0

// rawWindow is one CGWindowList entry as read from its CFDictionary, before
// any filtering, so the filtering rules below can be unit tested without
// CoreGraphics.
type rawWindow struct {
	pid      int32
	hasPID   bool
	layer    int32
	hasLayer bool
	name     string
}

// filterWindows keeps only the entries VisibleWindows reports: normal-layer
// windows with a known owner and a non-empty title.
//
// Untitled windows are dropped because they carry no signal — the same rule
// platform/windows applies. That includes every window whose title macOS
// hid because Screen Recording isn't granted: process and microphone
// evidence still flow in that case, so detection degrades rather than
// stops. A window without a layer entry is treated as not normal-layer:
// CoreGraphics always sets kCGWindowLayer, so a missing one means the entry
// is not something this code understands.
func filterWindows(raw []rawWindow) []WindowInfo {
	out := make([]WindowInfo, 0, len(raw))
	for _, w := range raw {
		if !w.hasPID || w.pid <= 0 {
			continue
		}
		if !w.hasLayer || w.layer != normalWindowLayer {
			continue
		}
		if w.name == "" {
			continue // untitled windows carry no signal
		}
		out = append(out, WindowInfo{PID: uint32(w.pid), Title: w.name})
	}
	return out
}
