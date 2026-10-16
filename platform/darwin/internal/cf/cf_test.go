//go:build darwin

package cf

import (
	"sync"
	"testing"
)

var coreGraphics = NewLibrary("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")

func TestReady(t *testing.T) {
	if err := Ready(); err != nil {
		t.Fatalf("Ready() = %v; CoreFoundation should always load on macOS", err)
	}
}

// TestGlobalString reads an exported CFStringRef constant and converts it.
// CoreGraphics' window-dictionary keys are CFStrings whose value is their
// own name, which makes them a fixture that needs no CFString creation.
func TestGlobalString(t *testing.T) {
	key, err := coreGraphics.Global("kCGWindowName")
	if err != nil {
		t.Fatalf("Global(kCGWindowName): %v", err)
	}
	if got := String(key); got != "kCGWindowName" {
		t.Errorf("String(kCGWindowName) = %q, want %q", got, "kCGWindowName")
	}
	// A CFString is not a number, an array or a dictionary: the typed
	// accessors must say so rather than misread it.
	if _, ok := Int32(key); ok {
		t.Error("Int32(CFString) reported ok")
	}
	if n := ArrayLen(key); n != 0 {
		t.Errorf("ArrayLen(CFString) = %d, want 0", n)
	}
	if IsDictionary(key) {
		t.Error("IsDictionary(CFString) = true")
	}
}

func TestNullRefs(t *testing.T) {
	Release(0) // must not crash
	if got := String(0); got != "" {
		t.Errorf("String(0) = %q", got)
	}
	if _, ok := Int32(0); ok {
		t.Error("Int32(0) reported ok")
	}
	if _, ok := Bool(0); ok {
		t.Error("Bool(0) reported ok")
	}
	if got := ArrayAt(0, 0); got != 0 {
		t.Errorf("ArrayAt(0, 0) = %#x", got)
	}
	if got := DictGet(0, 1); got != 0 {
		t.Errorf("DictGet(0, 1) = %#x", got)
	}
}

func TestMissingLibraryAndSymbol(t *testing.T) {
	missing := NewLibrary("/System/Library/Frameworks/DoesNotExist.framework/DoesNotExist")
	if _, err := missing.Func("Nothing", Void).Call(); err == nil {
		t.Error("Call on a missing library succeeded")
	}
	if _, err := missing.Global("kNothing"); err == nil {
		t.Error("Global on a missing library succeeded")
	}
	if _, err := CoreFoundation.Func("CFNoSuchFunction", Void).Call(); err == nil {
		t.Error("Call on a missing symbol succeeded")
	}
}

func TestCallArgumentCount(t *testing.T) {
	if _, err := cfGetTypeID.Call(); err == nil {
		t.Error("Call with too few arguments succeeded")
	}
	if _, err := CoreFoundation.Func("CFRelease", Void, Pointer, Pointer, Pointer, Pointer, Pointer, Pointer, Pointer, Pointer, Pointer).Call(); err == nil {
		t.Error("Func with more than maxArgs arguments prepared")
	}
}

// TestConcurrentFirstUse races many goroutines on a never-used Library and
// Func, checking under -race that lazy loading is properly synchronised.
func TestConcurrentFirstUse(t *testing.T) {
	lib := NewLibrary("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
	key := lib.Func("CGMainDisplayID", U32)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := key.Call(); err != nil {
				t.Errorf("CGMainDisplayID: %v", err)
			}
			if s := String(mustGlobal(t, lib, "kCGWindowOwnerPID")); s != "kCGWindowOwnerPID" {
				t.Errorf("String(kCGWindowOwnerPID) = %q", s)
			}
		}()
	}
	wg.Wait()
}

func mustGlobal(t *testing.T, lib *Library, name string) Ref {
	t.Helper()
	r, err := lib.Global(name)
	if err != nil {
		t.Errorf("Global(%s): %v", name, err)
	}
	return r
}
