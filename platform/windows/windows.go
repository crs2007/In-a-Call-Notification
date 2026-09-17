//go:build windows

package windows

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// enumUser32 loads user32.dll independently of dialog.go's own handle:
// LazyDLL.NewProc is safe to call any number of times, but keeping this
// file's syscalls self-contained avoids coupling window enumeration to the
// broker dialog's plumbing.
var enumUser32 = windows.NewLazySystemDLL("user32.dll")

var (
	procEnumWindows              = enumUser32.NewProc("EnumWindows")
	procEnumGetWindowTextW       = enumUser32.NewProc("GetWindowTextW")
	procEnumGetWindowTextLengthW = enumUser32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible          = enumUser32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = enumUser32.NewProc("GetWindowThreadProcessId")
)

// WindowInfo is a single top-level window: which process owns it, and its
// title. Titles often carry meeting names, so callers must never log them
// above slog.Debug.
type WindowInfo struct {
	PID   uint32
	Title string
}

// enumMu serialises access to enumBuffer across concurrent VisibleWindows
// calls.
//
// EnumWindows is synchronous, so a single call's reset-call-copy sequence is
// safe on its own. But VisibleWindows can itself be called concurrently by
// more than one engine generation's poll loop — e.g. during a
// supervisor-driven reload, the old generation's poll can still be mid-flight
// (inside a detector, inside VisibleWindows) when the new generation starts
// its own poll. Without a lock, one call's `enumBuffer = enumBuffer[:0]`
// races with another's in-flight `append`. The mutex makes concurrent callers
// serialize instead.
var enumMu sync.Mutex

// enumBuffer accumulates results for the in-flight EnumWindows call.
//
// syscall.NewCallback allocates a callback slot that is never released, and
// the process is capped at a few thousand of them. A long-running poll loop
// must therefore create the callback exactly once, at package level, and
// share state through a package variable rather than a closure per call.
// Access to this buffer is guarded by enumMu; see its comment for why.
var enumBuffer []WindowInfo

var enumCallback = syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
	const continueEnumeration = 1

	if visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); visible == 0 {
		return continueEnumeration
	}
	length, _, _ := procEnumGetWindowTextLengthW.Call(uintptr(hwnd))
	if length == 0 {
		return continueEnumeration // untitled windows carry no signal
	}

	buf := make([]uint16, length+1)
	procEnumGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), length+1)

	var pid uint32
	procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))

	enumBuffer = append(enumBuffer, WindowInfo{PID: pid, Title: windows.UTF16ToString(buf)})
	return continueEnumeration
})

// VisibleWindows enumerates every visible, titled top-level window on the
// desktop. It is a raw observation: whether any of these titles means the
// user is "in a call" is a rule owned by internal/detectors, not this
// package.
func VisibleWindows() []WindowInfo {
	enumMu.Lock()
	defer enumMu.Unlock()

	enumBuffer = enumBuffer[:0]
	procEnumWindows.Call(enumCallback, 0)
	out := make([]WindowInfo, len(enumBuffer))
	copy(out, enumBuffer)
	return out
}
