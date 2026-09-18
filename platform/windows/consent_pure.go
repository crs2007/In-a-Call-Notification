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

// consentEntry is one ConsentStore subkey's identity and liveness, gathered
// from the registry by devicesInUse (consent.go) so the "is its owning
// process actually still running" filtering below can be unit tested without
// touching the registry at all.
type consentEntry struct {
	// name is the value devicesInUse would report: an unmangled exe path for
	// NonPackaged entries, a package family name for packaged ones.
	name string
	// nonPackaged is false for packaged (MSIX) apps, keyed by package family
	// name directly under the device key.
	nonPackaged bool
	// live is consentEntryIsLive's verdict on this entry's LastUsedTimeStop.
	live bool
}

// filterLiveConsentEntries drops ConsentStore entries that claim to be live
// but whose owning process is not in runningExeNames — a stale or orphaned
// entry (the owning app crashed, or Windows never wrote a Stop time) must
// not be reported as "in use" forever.
//
// Only NonPackaged entries are checked this way: their name is an exe path,
// directly comparable (by base filename, case-insensitively — Windows exe
// matching is case-insensitive) to a running process's name.
//
// Packaged (MSIX) entries — e.g. New Teams — are keyed by package family
// name (like "MSTeams_8wekyb3d8bbwe"), which process.go's Toolhelp32-based
// ProcessNames has no way to produce: that would need GetPackageFamilyName
// against an OpenProcess handle for every running PID, every poll, which is
// materially more Win32 surface than this filtering step otherwise needs. So
// packaged entries are passed through unfiltered here, same as before this
// function existed — a live packaged entry is still trusted at face value.
// This is a known, deliberate gap, not an oversight.
//
// runningExeNames must hold lowercased base executable filenames only (e.g.
// "ms-teams.exe"), which is exactly what runningExeNameSet builds from
// ProcessNames' output.
func filterLiveConsentEntries(entries []consentEntry, runningExeNames map[string]struct{}) []string {
	var inUse []string
	for _, e := range entries {
		if !e.live {
			continue
		}
		if e.nonPackaged {
			if _, ok := runningExeNames[baseExeNameLower(e.name)]; !ok {
				continue
			}
		}
		inUse = append(inUse, e.name)
	}
	return inUse
}

// runningExeNameSet turns a PID->exe-name map (ProcessNames' shape) into a
// lowercased set of base filenames, built once per devicesInUse call rather
// than once per ConsentStore entry.
func runningExeNameSet(procNames map[uint32]string) map[string]struct{} {
	set := make(map[string]struct{}, len(procNames))
	for _, name := range procNames {
		set[baseExeNameLower(name)] = struct{}{}
	}
	return set
}

// baseExeNameLower returns the final path component of a Windows-style path
// (backslash-separated), lowercased. It deliberately does not use
// path/filepath: that package splits on the host OS's separator, and this
// must parse Windows paths correctly even when this file's tests run on a
// non-Windows GOOS.
func baseExeNameLower(path string) string {
	if i := strings.LastIndexByte(path, '\\'); i >= 0 {
		path = path[i+1:]
	}
	return strings.ToLower(path)
}
