//go:build windows

package windows

import (
	"log/slog"
	"sort"
	"strings"

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
// A ConsentStore entry claiming to be live is cross-checked against
// procNames (see filterLiveConsentEntries) so a stale entry left behind by a
// crashed or long-closed app doesn't get reported as "in use" forever — but
// only for NonPackaged entries; see filterLiveConsentEntries's doc comment
// for why packaged entries are not filtered this way.
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
						entries = append(entries, consentEntry{
							name:        unmangleNonPackagedKey(app),
							nonPackaged: true,
							live:        isLive(nonPackaged, app),
						})
					}
				}
				nonPackaged.Close()
				continue
			}
			entries = append(entries, consentEntry{name: name, live: isLive(key, name)})
		}
		key.Close()
	}

	inUse := filterLiveConsentEntries(entries, runningExeNameSet(procNames))
	sort.Strings(inUse)
	return inUse
}

// isLive opens subkey under parent and reports whether its LastUsedTimeStop
// says the application is using the device right now.
func isLive(parent registry.Key, subkey string) bool {
	key, err := registry.OpenKey(parent, subkey, registry.QUERY_VALUE)
	if err != nil {
		slog.Debug("consent entry unreadable", "err", err)
		return false
	}
	defer key.Close()

	stop, _, err := key.GetIntegerValue("LastUsedTimeStop")
	if err != nil {
		slog.Debug("LastUsedTimeStop unreadable", "err", err)
		return false
	}
	return consentEntryIsLive(stop)
}
