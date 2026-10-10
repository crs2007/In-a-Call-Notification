// Package cf is the small foreign-function layer platform/darwin uses to call
// macOS frameworks (CoreFoundation, CoreGraphics, CoreAudio, CoreMediaIO,
// libSystem) without cgo.
//
// It wraps github.com/go-webgpu/goffi — already in the module graph through
// the systray darwin backend — rather than adding purego or cgo, so the
// shipped binary still builds with CGO_ENABLED=0 and cross-compiles from any
// host.
//
// The package deliberately stays tiny: lazily loaded libraries, a typed
// function handle that prepares its call interface once and is then called
// any number of times, and just enough CoreFoundation (CFString, CFNumber,
// CFArray, CFDictionary, CFRelease) to read what the signal adapters need.
// Every entry point reports failure as an error or a zero value — never a
// panic — so a missing framework or symbol degrades detection to "no
// evidence" instead of killing the agent.
//
// Only the darwin build has any code; on other operating systems the package
// is empty so `go vet ./...` stays green everywhere.
package cf
