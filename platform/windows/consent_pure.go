package windows

import "strings"

// unmangleNonPackagedKey turns a NonPackaged ConsentStore subkey name back
// into the executable path it names. Windows stores these keys with "#" in
// place of "\" so the name can live directly under the registry hive.
//
// This is pure string manipulation with no registry or syscall dependency,
// so it builds and tests on every platform.
func unmangleNonPackagedKey(name string) string {
	return strings.ReplaceAll(name, "#", `\`)
}

// consentEntryIsLive reports whether a ConsentStore LastUsedTimeStop value of
// stop means the application is using the capability device right now. A
// stop time of zero means the application started using the device and has
// not yet stopped.
func consentEntryIsLive(stop uint64) bool {
	return stop == 0
}
