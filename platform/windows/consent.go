//go:build windows

package windows

import (
	"log/slog"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// consentStorePath is where Windows records which applications have used the
// microphone and camera, and — crucially — which are using them right now.
const consentStorePath = `SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\`

// AppsUsingMicrophone reports the applications currently holding the
// microphone, per HKCU and HKLM's ConsentStore, filtered to those whose
// owning process is still in procNames (see devicesInUse). A missing or
// unreadable key means "no evidence", not failure: this always returns a
// slice, even if empty, and never a fatal error.
//
// procNames is a PID->exe-name map shaped exactly like ProcessNames'
// return value; callers that already called ProcessNames this poll cycle
// (as internal/detectors does) should reuse it rather than taking a second
// snapshot.
func AppsUsingMicrophone(procNames map[uint32]string) []string {
	return devicesInUse("microphone", procNames)
}

// AppsUsingWebcam reports the applications currently holding the camera. See
// AppsUsingMicrophone for the shared ConsentStore mechanics and procNames'
// meaning.
func AppsUsingWebcam(procNames map[uint32]string) []string {
	return devicesInUse("webcam", procNames)
}

// devicesInUse reports the applications currently holding the named
// capability device ("microphone" or "webcam").
//
// Each application has a subkey holding LastUsedTimeStart and
// LastUsedTimeStop as FILETIMEs; consentEntryIsLive interprets the latter.
// Packaged (MSIX) applications such as New Teams sit directly under the
// device key, keyed by package family name; everything else sits under
// NonPackaged, keyed by its executable path with backslashes replaced by
// "#" (see unmangleNonPackagedKey). Names are returned as-is: whether to
// annotate packaged apps for display is a presentation decision that
// belongs to the caller, not this package.
//
// A ConsentStore entry claiming to be live is cross-checked against the
// running processes (see filterLiveConsentEntries) so a stale entry left
// behind by a crashed or long-closed app doesn't get reported as "in use"
// forever. procNames supplies the PIDs; runningProcesses resolves their
// creation times and package family names on demand.
//
// A missing key means "no evidence", never an error — the agent must keep
// running with degraded detection rather than fail.
func devicesInUse(device string, procNames map[uint32]string) []string {
	var entries []consentEntry

	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, consentStorePath+device, registry.READ)
		if err != nil {
			slog.Debug("consent store key not found", "device", device, "err", err)
			continue
		}

		names, err := key.ReadSubKeyNames(-1)
		if err != nil {
			slog.Debug("consent store subkeys unreadable", "device", device, "err", err)
			key.Close()
			continue
		}

		for _, name := range names {
			if strings.EqualFold(name, "NonPackaged") {
				nonPackaged, err := registry.OpenKey(key, name, registry.READ)
				if err != nil {
					slog.Debug("NonPackaged key unreadable", "device", device, "err", err)
					continue
				}
				appNames, err := nonPackaged.ReadSubKeyNames(-1)
				if err == nil {
					for _, app := range appNames {
						live, start := readConsentTimes(nonPackaged, app)
						entries = append(entries, consentEntry{
							name:        unmangleNonPackagedKey(app),
							nonPackaged: true,
							live:        live,
							start:       start,
						})
					}
				}
				nonPackaged.Close()
				continue
			}
			live, start := readConsentTimes(key, name)
			entries = append(entries, consentEntry{name: name, live: live, start: start})
		}
		key.Close()
	}

	inUse := filterLiveConsentEntries(entries, func() []runningProcess { return runningProcesses(procNames) })
	sort.Strings(inUse)
	return inUse
}

// readConsentTimes opens subkey under parent and reports whether its
// LastUsedTimeStop says the application is using the device right now, plus
// its LastUsedTimeStart FILETIME (0 if unreadable) for the start-time check
// in filterLiveConsentEntries.
func readConsentTimes(parent registry.Key, subkey string) (live bool, start uint64) {
	key, err := registry.OpenKey(parent, subkey, registry.QUERY_VALUE)
	if err != nil {
		slog.Debug("consent entry unreadable", "err", err)
		return false, 0
	}
	defer key.Close()

	stop, _, err := key.GetIntegerValue("LastUsedTimeStop")
	if err != nil {
		slog.Debug("LastUsedTimeStop unreadable", "err", err)
		return false, 0
	}
	if !consentEntryIsLive(stop) {
		return false, 0
	}
	start, _, err = key.GetIntegerValue("LastUsedTimeStart")
	if err != nil {
		slog.Debug("LastUsedTimeStart unreadable", "err", err)
		return true, 0
	}
	return true, start
}

var procGetPackageFamilyName = kernel32.NewProc("GetPackageFamilyName")

// appmodelErrorNoPackage is APPMODEL_ERROR_NO_PACKAGE from winerror.h:
// GetPackageFamilyName's answer for an ordinary, non-MSIX process.
const appmodelErrorNoPackage = 15700

// packageFamilyNameMaxLength is PACKAGE_FAMILY_NAME_MAX_LENGTH from
// appmodel.h (64), plus the terminating NUL.
const packageFamilyNameMaxLength = 64 + 1

// runningProcesses describes every PID in procNames the way ConsentStore
// entries name their owners: base exe name, package family name and creation
// time. It costs one OpenProcess per PID, so devicesInUse only invokes it
// (through filterLiveConsentEntries) when some ConsentStore entry claims to
// be live - i.e. during a call, or for the stale entry this exists to catch.
//
// A PID that cannot be opened (it exited between the snapshot and now, or is
// a protected process) is skipped: it cannot be the owner of a capture we
// could act on anyway. A process whose creation time or family cannot be
// read is still listed, with that field zeroed, which filterLiveConsentEntries
// treats as "unknown, do not check" rather than as evidence either way.
func runningProcesses(procNames map[uint32]string) []runningProcess {
	procs := make([]runningProcess, 0, len(procNames))
	for pid, name := range procNames {
		if pid == 0 {
			continue // the System Idle Process cannot be opened
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue
		}
		p := runningProcess{exe: baseExeNameLower(name)}

		var creation, exit, kernel, user windows.Filetime
		if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil {
			p.start = uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
		} else {
			slog.Debug("GetProcessTimes failed", "pid", pid, "err", err)
		}

		p.family = packageFamilyName(h, pid)
		windows.CloseHandle(h)
		procs = append(procs, p)
	}
	return procs
}

// packageFamilyName returns the MSIX package family name of the process
// behind h, or "" if it is not a packaged app (or the name is unreadable).
func packageFamilyName(h windows.Handle, pid uint32) string {
	var buf [packageFamilyNameMaxLength]uint16
	length := uint32(len(buf))
	rc, _, _ := procGetPackageFamilyName.Call(uintptr(h), uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buf[0])))
	switch rc {
	case 0:
		return windows.UTF16ToString(buf[:])
	case appmodelErrorNoPackage:
		return ""
	default:
		slog.Debug("GetPackageFamilyName failed", "pid", pid, "err", windows.Errno(rc))
		return ""
	}
}
