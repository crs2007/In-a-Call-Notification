package darwin

import "sort"

// fourCC packs a four-character code the way Core Audio and CoreMediaIO
// spell their property selectors and scopes ('prs#', 'glob', ...): the
// first character in the most significant byte. Codes that are not exactly
// four bytes are a programming error and encode as 0, which no property
// uses.
func fourCC(code string) uint32 {
	if len(code) != 4 {
		return 0
	}
	return uint32(code[0])<<24 | uint32(code[1])<<16 | uint32(code[2])<<8 | uint32(code[3])
}

// Core Audio and CoreMediaIO property selectors, scopes and objects. They
// are spelled out as numbers because Go can't evaluate fourCC at compile
// time; TestFourCCConstants pins each to its four-character code so a typo
// can't slip in.
const (
	// systemObject is kAudioObjectSystemObject and kCMIOObjectSystemObject:
	// the root object whose properties list processes and devices.
	systemObject = 1

	scopeGlobal = 0x676c6f62 // 'glob' kAudioObjectPropertyScopeGlobal / kCMIOObjectPropertyScopeGlobal
	elementMain = 0          // kAudioObjectPropertyElementMain / kCMIOObjectPropertyElementMain

	// selProcessObjectList ('prs#', kAudioHardwarePropertyProcessObjectList,
	// macOS 14.2+) lists one Core Audio object per process using audio.
	selProcessObjectList = 0x70727323
	// selProcessPID ('ppid', kAudioProcessPropertyPID) is that process's pid_t.
	selProcessPID = 0x70706964
	// selProcessBundleID ('pbid', kAudioProcessPropertyBundleID) is a
	// retained CFStringRef the caller must release.
	selProcessBundleID = 0x70626964
	// selProcessIsRunningInput ('piri', kAudioProcessPropertyIsRunningInput)
	// is non-zero while the process is capturing from any input device.
	selProcessIsRunningInput = 0x70697269
	// selProcessIsRunningOutput ('piro', kAudioProcessPropertyIsRunningOutput)
	// is non-zero while the process plays audio. Not used for detection —
	// music playing is not a call — but kept for the probe and tests.
	selProcessIsRunningOutput = 0x7069726f

	// selDefaultInputDevice ('dIn ', kAudioHardwarePropertyDefaultInputDevice)
	// is the AudioDeviceID of the current default microphone.
	selDefaultInputDevice = 0x64496e20
	// selDeviceIsRunningSomewhere ('gone', kAudioDevicePropertyDeviceIsRunningSomewhere
	// and kCMIODevicePropertyDeviceIsRunningSomewhere) is non-zero while any
	// process on the system is using the device.
	selDeviceIsRunningSomewhere = 0x676f6e65

	// selCMIODevices ('dev#', kCMIOHardwarePropertyDevices) lists every
	// CoreMediaIO capture device (cameras, virtual cameras, capture cards).
	selCMIODevices = 0x64657623
)

// unknownApp is reported in place of an application name when the OS can
// only say that *some* process holds a device: on macOS before 14.2 for
// the microphone, and always for the camera. Rules can match it explicitly
// and give it a low weight, since it can't distinguish a call from, say, a
// dictation app.
const unknownApp = "(unknown)"

// audioProcess is one Core Audio process object as read from the HAL, so
// the naming and filtering below can be unit tested without Core Audio.
type audioProcess struct {
	pid          int32
	bundleID     string
	inputRunning bool
}

// microphoneUsers turns Core Audio process objects into AppsUsingMicrophone's
// result: the sorted, de-duplicated names of processes currently capturing
// input.
//
// The name is the bundle ID (e.g. "com.microsoft.teams2", "us.zoom.xos")
// because that is stable across versions and install locations, and it is
// what Core Audio attributes the input to. A process with no bundle — a
// command-line tool, or a helper Core Audio couldn't attribute — falls back
// to its executable name from procNames, and failing that to unknownApp: the
// microphone is still in use, so dropping the entry would hide real
// evidence. Several process objects can share a bundle ID (an app and its
// helpers), hence the de-duplication.
func microphoneUsers(procs []audioProcess, procNames map[uint32]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, p := range procs {
		if !p.inputRunning {
			continue
		}
		name := p.bundleID
		if name == "" && p.pid > 0 {
			name = procNames[uint32(p.pid)]
		}
		if name == "" {
			name = unknownApp
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// deviceLevelUsers is the result for a device that can only say whether
// *anything* is using it: unknownApp when running, nil otherwise.
func deviceLevelUsers(runningSomewhere bool) []string {
	if runningSomewhere {
		return []string{unknownApp}
	}
	return nil
}
