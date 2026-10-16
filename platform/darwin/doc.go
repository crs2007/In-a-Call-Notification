// Package darwin holds every macOS system call CallMQTT makes. Nothing
// outside this package talks to the OS directly, and nothing in internal/
// imports it: platform code returns raw data and leaves every decision to
// the caller, exactly as platform/windows does.
//
// Frameworks (CoreFoundation, CoreGraphics, CoreAudio, CoreMediaIO, libproc)
// are called through goffi, so the binary still builds with CGO_ENABLED=0.
// Files without a build tag hold pure logic that tests on every OS.
package darwin
