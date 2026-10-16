# Patched systray module

This directory contains `github.com/gogpu/systray` v0.3.0 with two fixes:

- **Windows:** notification icons using `NOTIFYICON_VERSION_4` include
  `NIF_SHOWTIP` when they are added or updated. Without that flag, Windows
  suppresses the standard hover tooltip.
- **macOS** (`internal/platform_darwin.go`): `SetIcon`, `SetTemplateIcon` and
  `SetTooltip` called off the main thread now hand the newest value to the
  main thread (`performSelectorOnMainThread:`, `waitUntilDone:NO`) instead
  of mutating the status item button from the caller's goroutine, which
  AppKit does not allow. Calls on the main thread still apply at once. The
  `NSString`/`NSImage` objects created for the button and for menu item
  updates are released once AppKit holds its own reference; before, every
  tooltip and menu label refresh leaked one string.

Remove the root `replace` directive and this directory after both fixes are
available in a released upstream version.
