//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestFirstLaunchPackagedGUI launches the binary the way a user does on a
// fresh install: the tray build, linked -H=windowsgui exactly as
// .goreleaser.yaml ships it, double-clicked with no config anywhere. A
// double-click hands the process no std handles at all, so a startup
// failure written to stderr goes nowhere and the process just exits —
// which is how v0.1.0-alpha.1 shipped (issue #1). The test asserts what a
// person would see: a dialog titled appTitle that names the config path, a
// starter config written there, and an exit code of 1 once the dialog is
// dismissed. It drives the real Win32 window rather than a hook in the
// binary so a regression in main's fallback, in the linker flags or in the
// tray build itself all fail it.
//
// It builds the exe, so it is skipped under -short.
func TestFirstLaunchPackagedGUI(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches the tray exe; skipped under -short")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, "callmqtt.exe")
	build := exec.Command(goBin, "build", "-tags", "tray", "-trimpath", "-ldflags", "-s -w -H=windowsgui", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build tray exe: %v\n%s", err, out)
	}

	// A path the exe has never seen, two directories deep, so the test also
	// covers creating %APPDATA%\callmqtt on a machine that has never run it.
	cfgPath := filepath.Join(dir, "AppData", "callmqtt", "config.yaml")
	proc := startWithoutStdHandles(t, exe, "--config", cfgPath)
	// exited is closed once the process is gone; state is valid after that.
	exited := make(chan struct{})
	var state *os.ProcessState
	go func() {
		state, _ = proc.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			_ = proc.Kill()
			<-exited
		}
	})

	hwnd := waitForDialog(t, proc.Pid, exited)

	if got := dialogText(hwnd); !strings.Contains(got, cfgPath) {
		t.Errorf("dialog text does not name the config path %s:\n%s", cfgPath, got)
	}
	if got, err := os.ReadFile(cfgPath); err != nil {
		t.Errorf("starter config was not written: %v", err)
	} else if !strings.Contains(string(got), "allowed_networks:") {
		t.Errorf("starter config at %s is not the packaged example", cfgPath)
	}

	// Dismissing the dialog lets the process finish; it must report failure,
	// since nothing ran.
	postMessage(hwnd, wmClose)
	select {
	case <-exited:
		if state == nil || state.ExitCode() != 1 {
			t.Errorf("exit after dismissing the dialog: %v, want exit status 1", state)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("process did not exit within 10s of the dialog being closed")
	}
}

// startWithoutStdHandles starts exe the way Explorer, the Start menu or a
// login entry does: with no stdin, stdout or stderr. exec.Command would
// quietly hand the child NUL for each instead, and a write to NUL succeeds,
// so the fallback under test would never trigger.
func startWithoutStdHandles(t *testing.T, exe string, args ...string) *os.Process {
	t.Helper()
	pid, handle, err := syscall.StartProcess(exe, append([]string{exe}, args...), &syscall.ProcAttr{
		Files: []uintptr{0, 0, 0},
	})
	if err != nil {
		t.Fatalf("start %s: %v", exe, err)
	}
	syscall.CloseHandle(syscall.Handle(handle))
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find process %d: %v", pid, err)
	}
	return proc
}

// waitForDialog polls for a MessageBox (window class #32770) titled appTitle
// owned by pid, failing the test if the process exits or 20s pass first.
func waitForDialog(t *testing.T, pid int, exited <-chan struct{}) uintptr {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if hwnd := findDialog(pid, appTitle); hwnd != 0 {
			return hwnd
		}
		select {
		case <-exited:
			t.Fatalf("process exited without showing a %q dialog: a missing config made the packaged GUI build fail invisibly", appTitle)
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q dialog appeared within 20s", appTitle)
		}
	}
}

// --- Win32 plumbing, test-only ------------------------------------------

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW            = user32.NewProc("FindWindowExW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetDlgItemTextW          = user32.NewProc("GetDlgItemTextW")
	procPostMessageW             = user32.NewProc("PostMessageW")
)

const (
	dialogClass  = "#32770" // the class every MessageBox window has
	staticTextID = 0xFFFF   // control id MessageBox gives its text
	wmClose      = 0x0010
)

// findDialog returns the first top-level dialog titled title that belongs to
// pid, or 0. The pid check keeps a developer's own running copy of the app,
// or a second test process, from being mistaken for the one under test.
func findDialog(pid int, title string) uintptr {
	class := windows.StringToUTF16Ptr(dialogClass)
	name := windows.StringToUTF16Ptr(title)
	var hwnd uintptr
	for {
		hwnd, _, _ = procFindWindowExW.Call(0, hwnd, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(name)))
		if hwnd == 0 {
			return 0
		}
		var owner uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if int(owner) == pid {
			return hwnd
		}
	}
}

func dialogText(hwnd uintptr) string {
	buf := make([]uint16, 4096)
	n, _, _ := procGetDlgItemTextW.Call(hwnd, staticTextID, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func postMessage(hwnd uintptr, msg uint32) {
	procPostMessageW.Call(hwnd, uintptr(msg), 0, 0)
}
