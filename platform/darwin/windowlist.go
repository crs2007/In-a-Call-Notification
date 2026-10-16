//go:build darwin

package darwin

import (
	"log/slog"
	"sync"

	"github.com/crs2007/callmqtt/platform/darwin/internal/cf"
)

// CGWindowListCopyWindowInfo(option, relativeToWindow) returns a CFArray of
// CFDictionaries, one per window, which the caller owns and must release.
var cgWindowListCopyWindowInfo = coreGraphicsLib.Func("CGWindowListCopyWindowInfo", cf.Pointer, cf.U32, cf.U32)

const (
	windowListOnScreenOnly          = 1 << 0 // kCGWindowListOptionOnScreenOnly
	windowListExcludeDesktopElement = 1 << 4 // kCGWindowListExcludeDesktopElements
	nullWindowID                    = 0      // kCGNullWindowID: no reference window
)

// windowKeys are CoreGraphics' exported CFStringRef dictionary keys, read
// once from the framework's globals. They are constants owned by
// CoreGraphics: never released.
var windowKeys struct {
	once                  sync.Once
	ownerPID, layer, name cf.Ref
	err                   error
}

func loadWindowKeys() error {
	windowKeys.once.Do(func() {
		for _, k := range []struct {
			symbol string
			dst    *cf.Ref
		}{
			{"kCGWindowOwnerPID", &windowKeys.ownerPID},
			{"kCGWindowLayer", &windowKeys.layer},
			{"kCGWindowName", &windowKeys.name},
		} {
			v, err := coreGraphicsLib.Global(k.symbol)
			if err == nil && v == 0 {
				err = errNilSymbol(k.symbol)
			}
			if err != nil {
				windowKeys.err = err
				return
			}
			*k.dst = v
		}
	})
	return windowKeys.err
}

// VisibleWindows reports every on-screen, normal-layer window with its
// owning PID and title (empty when macOS withholds it). It is a raw
// observation: whether any of these titles means the user is "in a call" is
// a rule owned by internal/rules, not this package.
//
// It reads CoreGraphics' window list (CGWindowListCopyWindowInfo), which
// needs no permission for PIDs and layers but — since macOS 10.15 — returns
// window titles only to processes granted Screen Recording. Without it,
// almost every title comes back empty but the window is still reported for
// its owner; noteScreenRecordingState logs that once, not every poll.
// Detection then leans on process and microphone evidence instead.
//
// Every failure (CoreGraphics or CoreFoundation unavailable, a NULL list)
// means "no evidence": nil, never an error or a panic.
//
// Safe for concurrent use: CGWindowList and the CF accessors are
// thread-safe, the result is built in a call-local slice, and the only
// shared state (dictionary keys, the permission log state) is
// initialised under sync.Once or updated atomically.
func VisibleWindows() []WindowInfo {
	if err := cf.Ready(); err != nil {
		signalUnavailable("window titles (CoreFoundation)", err)
		return nil
	}
	if err := loadWindowKeys(); err != nil {
		signalUnavailable("window titles (CoreGraphics keys)", err)
		return nil
	}
	if err := cgWindowListCopyWindowInfo.Load(); err != nil {
		signalUnavailable("window titles (CGWindowListCopyWindowInfo)", err)
		return nil
	}
	noteScreenRecordingState()

	list, err := cgWindowListCopyWindowInfo.Call(windowListOnScreenOnly|windowListExcludeDesktopElement, nullWindowID)
	if err != nil || list == 0 {
		slog.Debug("CGWindowListCopyWindowInfo returned no list", "err", err)
		return nil
	}
	arr := cf.Ref(list)
	defer cf.Release(arr)

	n := cf.ArrayLen(arr)
	raw := make([]rawWindow, 0, n)
	for i := 0; i < n; i++ {
		d := cf.ArrayAt(arr, i) // borrowed: lives as long as arr
		var w rawWindow
		w.pid, w.hasPID = cf.Int32(cf.DictGet(d, windowKeys.ownerPID))
		w.layer, w.hasLayer = cf.Int32(cf.DictGet(d, windowKeys.layer))
		if w.hasLayer && w.layer == normalWindowLayer {
			// Only convert titles of windows filterWindows keeps:
			// string conversion is the expensive part of the walk.
			w.name = cf.String(cf.DictGet(d, windowKeys.name))
		}
		raw = append(raw, w)
	}
	return filterWindows(raw)
}
