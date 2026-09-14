//go:build windows

package windows

import (
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

// enumBuffer accumulates results for the in-flight EnumWindows call.
//
// syscall.NewCallback allocates a callback slot that is never released, and
// the process is capped at a few thousand of them. A long-running poll loop
// must therefore create the callback exactly once, at package level, and
// share state through a package variable rather than a closure per call.
// EnumWindows is synchronous, so a single unsynchronised buffer is safe here.
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
	enumBuffer = enumBuffer[:0]
	procEnumWindows.Call(enumCallback, 0)
	out := make([]WindowInfo, len(enumBuffer))
	copy(out, enumBuffer)
	return out
}
