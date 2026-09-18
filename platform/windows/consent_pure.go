package windows

import (
	"strings"
	"time"
)

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
	// start is the entry's LastUsedTimeStart as a FILETIME (100 ns ticks
	// since 1601-01-01 UTC), the moment the app began using the device.
	// Zero means the value was missing or unreadable, which disables the
	// start-time check for this entry.
	start uint64
}

// runningProcess is one running process described the two ways a
// ConsentStore entry can name its owner: by base executable name (NonPackaged
// entries) and by package family name (packaged entries).
type runningProcess struct {
	// exe is the lowercased base executable filename, e.g. "ms-teams.exe".
	exe string
	// family is the MSIX package family name, e.g. "MSTeams_8wekyb3d8bbwe",
	// or "" when the process is not packaged or its family could not be read.
	family string
	// start is the process creation time as a FILETIME. Zero means unknown,
	// which disables the start-time check against this process.
	start uint64
}

// consentStartSlack is how far a ConsentStore LastUsedTimeStart may precede
// the creation time of the process it is credited to and still count as
// that process's own capture. It only exists to absorb a wall-clock step
// (w32time correcting a large offset) landing between the app starting and
// the capture starting; a capture genuinely cannot begin before the process
// that owns it. Ten seconds is generous for that and still far smaller than
// the time it takes to relaunch a crashed client.
const consentStartSlack = uint64(10 * time.Second / 100)

// filterLiveConsentEntries drops ConsentStore entries that claim to be live
// but cannot belong to any process running right now. A live entry is
// trusted only if some running process (a) is the entry's owner - same base
// exe name for NonPackaged entries, same package family name for packaged
// ones - and (b) was created no later than the entry's LastUsedTimeStart
// (within consentStartSlack).
//
// (a) covers the app that exited or crashed without Windows ever writing a
// Stop time. (b) covers the case (a) cannot: the app crashed mid-call and
// was relaunched, so it *is* running, but the entry's start time predates
// the relaunch - no current instance can have started that capture. That
// stale entry otherwise reads as "microphone in use" for hours or days,
// until the app's next real call overwrites it (issue #8).
//
// running is invoked at most once, and only if some entry is live: resolving
// creation times and package family names needs an OpenProcess handle per
// PID (see runningProcesses in consent.go), which is not worth paying on the
// idle polls that are the overwhelming majority. A nil running function, or
// one returning no processes, keeps every live entry - the "no evidence"
// posture consent.go takes for any unreadable platform source.
func filterLiveConsentEntries(entries []consentEntry, running func() []runningProcess) []string {
	var (
		inUse    []string
		procs    []runningProcess
		resolved bool
	)
	for _, e := range entries {
		if !e.live {
			continue
		}
		if !resolved {
			resolved = true
			if running != nil {
				procs = running()
			}
		}
		if len(procs) > 0 && !ownedByRunningProcess(e, procs) {
			continue
		}
		inUse = append(inUse, e.name)
	}
	return inUse
}

// ownedByRunningProcess reports whether some process in procs could have
// started the capture that entry e records: it must be e's owner by name,
// and must have existed when the capture began.
func ownedByRunningProcess(e consentEntry, procs []runningProcess) bool {
	owner := e.name
	if e.nonPackaged {
		owner = baseExeNameLower(e.name)
	}
	for _, p := range procs {
		var match bool
		if e.nonPackaged {
			match = p.exe == owner
		} else {
			match = p.family != "" && strings.EqualFold(p.family, owner)
		}
		if !match {
			continue
		}
		if e.start == 0 || p.start == 0 || p.start <= e.start+consentStartSlack {
			return true
		}
	}
	return false
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
