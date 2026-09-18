//go:build !windows && tray

package tray

// openFileOS is unreachable outside Windows: openFile only calls it when
// runtime.GOOS == "windows". It exists so this package still compiles on
// darwin/linux, where the tray build tag is used for tests and cross-build
// checks even though CallMQTT ships Windows-only.
func openFileOS(string) {}
