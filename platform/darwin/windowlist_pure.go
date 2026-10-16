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
// windows with a known owner.
//
// Untitled windows are kept, unlike platform/windows. Without Screen
// Recording macOS blanks almost every title, and the window is then the only
// place internal/rules learns that the app's process is on screen at all:
// dropping it would leave mic evidence alone, which never reaches the active
// threshold. An empty title never matches a title regex, so keeping it adds
// process evidence and nothing else. A window without a layer entry is
// treated as not normal-layer:
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
		out = append(out, WindowInfo{PID: uint32(w.pid), Title: w.name})
	}
	return out
}
