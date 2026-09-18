//go:build windows

package windows

import (
	"log/slog"
	"runtime"
	"sort"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file answers one question the ConsentStore cannot: which processes
// are *playing* audio right now. Windows records microphone/camera use in
// the registry (see consent.go) but keeps no such ledger for playback, so
// playback has to be read live from the audio engine via WASAPI's session
// manager (IAudioSessionManager2 -> IAudioSessionEnumerator ->
// IAudioSessionControl2::GetProcessId/GetState).
//
// It exists for cmd/probe captures (see AudioSessions for what the Google
// Meet captures showed and why no rule consumes it yet).
//
// No CGO: the COM interfaces are driven through raw vtable calls, the same
// way dialog.go drives user32 without a GUI toolkit.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

// COM identifiers from mmdeviceapi.h and audiopolicy.h.
var (
	clsidMMDeviceEnumerator   = mustGUID("{BCDE0395-E52F-467C-8E3D-C4579291692E}")
	iidIMMDeviceEnumerator    = mustGUID("{A95664D2-9614-4F35-A746-DE8DB63617E6}")
	iidIAudioSessionManager2  = mustGUID("{77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F}")
	iidIAudioSessionControl2  = mustGUID("{BFB7FF88-7239-4FC9-8FA2-07C950BE9C6D}")
	iidIAudioMeterInformation = mustGUID("{C02216F6-8C67-4B5B-9D00-D008E73E0064}")
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic("audio: bad GUID literal " + s + ": " + err.Error())
	}
	return g
}

const (
	eRender                 = 0    // EDataFlow: playback endpoints
	deviceStateActive       = 0x1  // DEVICE_STATE_ACTIVE
	audioSessionStateActive = 1    // AudioSessionState: a stream is open and running
	clsctxAll               = 0x17 // CLSCTX_ALL

	// Vtable slots. IUnknown occupies 0-2 on every interface.
	vtblEnumAudioEndpoints    = 3 // IMMDeviceEnumerator
	vtblCollectionGetCount    = 3 // IMMDeviceCollection
	vtblCollectionItem        = 4 // IMMDeviceCollection
	vtblDeviceActivate        = 3 // IMMDevice
	vtblGetSessionEnumerator  = 5 // IAudioSessionManager2
	vtblSessionEnumGetCount   = 3 // IAudioSessionEnumerator
	vtblSessionEnumGetSession = 4 // IAudioSessionEnumerator
	vtblSessionGetState       = 3 // IAudioSessionControl
	vtblSession2GetProcessId  = 14
	vtblSession2IsSystemSound = 15
	vtblMeterGetPeakValue     = 3 // IAudioMeterInformation
)

// comObject is the minimal shape shared by every COM interface pointer: a
// pointer to a vtable of function pointers. Only slots that the named
// interface actually has are ever indexed.
type comObject struct {
	vtbl *[32]uintptr
}

// call invokes the method in vtable slot on o. Callers pass out-parameters
// as uintptr(unsafe.Pointer(&x)) directly in the call expression; the
// uintptrescapes directive makes the compiler heap-allocate and keep those
// objects alive for the duration of the call, the same guarantee
// LazyProc.Call and syscall.SyscallN give and for the same reason (a
// stack-allocated x could otherwise move between the conversion and the
// syscall).
//
//go:uintptrescapes
func (o *comObject) call(slot int, args ...uintptr) uintptr {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(unsafe.Pointer(o)))
	all = append(all, args...)
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], all...)
	return hr
}

func (o *comObject) release() {
	if o != nil {
		o.call(2)
	}
}

func (o *comObject) queryInterface(iid *windows.GUID) (*comObject, bool) {
	var out *comObject
	hr := o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, hr == 0 && out != nil
}

// AudioSessions reports every process that has an *active* playback stream
// on any active output device right now, with the loudest instantaneous
// peak (0..1) across its sessions at sample time, sorted by exe name.
//
// procNames is the PID->exe-name map ProcessNames returned this poll; a
// session whose PID is not in it (the process has since exited, or it is
// the pid-0 system-sounds session) is dropped. Any failure means "no
// evidence": this returns an empty result, never an error.
//
// Today this is capture instrumentation only, printed by cmd/probe as
// [audio-out]; no detection rule scores it. It was added while looking for
// a signal that separates Google Meet's "Ready to join?" lobby from a joined
// call, and the captures (testdata/probe/meet-lobby.txt vs meet-in-call.txt)
// showed that an *open* stream does not: Chrome opens one as soon as the
// Meet page loads. The peak meter did differ (0.000 throughout the lobby,
// brief spikes in the call) but a 2-second instantaneous sample is far too
// sparse to score; a future "audible recently" rule would need a sampler.
// Kept so that future captures record it.
func AudioSessions(procNames map[uint32]string) []AudioSession {
	// CoUninitialize must run on the thread that initialised COM, so pin
	// this goroutine to its OS thread for the duration of the call.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	switch {
	case err == nil, err == syscall.Errno(1): // S_OK, or S_FALSE (already initialised on this thread)
		defer windows.CoUninitialize()
	case err == syscall.Errno(windows.RPC_E_CHANGED_MODE):
		// Thread already in an STA (the tray's message loop). Usable as-is,
		// but it is not ours to uninitialise.
	default:
		slog.Debug("audio: CoInitializeEx failed", "err", err)
		return nil
	}

	var enumerator *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enumerator)))
	if hr != 0 || enumerator == nil {
		slog.Debug("audio: CoCreateInstance(MMDeviceEnumerator) failed", "hresult", hr)
		return nil
	}
	defer enumerator.release()

	var devices *comObject
	if hr := enumerator.call(vtblEnumAudioEndpoints, eRender, deviceStateActive, uintptr(unsafe.Pointer(&devices))); hr != 0 || devices == nil {
		slog.Debug("audio: EnumAudioEndpoints failed", "hresult", hr)
		return nil
	}
	defer devices.release()

	var deviceCount uint32
	devices.call(vtblCollectionGetCount, uintptr(unsafe.Pointer(&deviceCount)))

	seen := map[string]float32{}
	for i := uint32(0); i < deviceCount; i++ {
		var device *comObject
		if hr := devices.call(vtblCollectionItem, uintptr(i), uintptr(unsafe.Pointer(&device))); hr != 0 || device == nil {
			continue
		}
		collectActiveSessions(device, procNames, seen)
		device.release()
	}

	out := make([]AudioSession, 0, len(seen))
	for name, peak := range seen {
		out = append(out, AudioSession{Exe: name, Peak: peak})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Exe < out[j].Exe })
	return out
}

// collectActiveSessions walks one output device's audio sessions and adds
// the exe name of every process with a session in AudioSessionStateActive
// to seen. Inactive and expired sessions (a player that is open but paused,
// or a stream that finished) are skipped: only a stream that is running
// right now counts as playback.
func collectActiveSessions(device *comObject, procNames map[uint32]string, seen map[string]float32) {
	var manager *comObject
	if hr := device.call(vtblDeviceActivate,
		uintptr(unsafe.Pointer(&iidIAudioSessionManager2)), clsctxAll, 0,
		uintptr(unsafe.Pointer(&manager))); hr != 0 || manager == nil {
		slog.Debug("audio: IMMDevice::Activate(IAudioSessionManager2) failed", "hresult", hr)
		return
	}
	defer manager.release()

	var sessions *comObject
	if hr := manager.call(vtblGetSessionEnumerator, uintptr(unsafe.Pointer(&sessions))); hr != 0 || sessions == nil {
		slog.Debug("audio: GetSessionEnumerator failed", "hresult", hr)
		return
	}
	defer sessions.release()

	var count int32
	sessions.call(vtblSessionEnumGetCount, uintptr(unsafe.Pointer(&count)))

	for i := int32(0); i < count; i++ {
		var control *comObject
		if hr := sessions.call(vtblSessionEnumGetSession, uintptr(i), uintptr(unsafe.Pointer(&control))); hr != 0 || control == nil {
			continue
		}
		if name, ok := activeSessionOwner(control, procNames); ok {
			if peak := sessionPeak(control); peak > seen[name] || !hasKey(seen, name) {
				seen[name] = peak
			}
		}
		control.release()
	}
}

// activeSessionOwner returns the exe name owning one session if, and only
// if, the session is active, belongs to a real process (not the system
// sounds pseudo-session), and that process is still running per procNames.
func activeSessionOwner(control *comObject, procNames map[uint32]string) (string, bool) {
	var state int32
	if hr := control.call(vtblSessionGetState, uintptr(unsafe.Pointer(&state))); hr != 0 || state != audioSessionStateActive {
		return "", false
	}

	control2, ok := control.queryInterface(&iidIAudioSessionControl2)
	if !ok {
		return "", false
	}
	defer control2.release()

	// IsSystemSoundsSession returns S_OK (0) for the system-sounds session
	// and S_FALSE (1) otherwise.
	if control2.call(vtblSession2IsSystemSound) == 0 {
		return "", false
	}

	var pid uint32
	if hr := control2.call(vtblSession2GetProcessId, uintptr(unsafe.Pointer(&pid))); hr != 0 || pid == 0 {
		return "", false
	}
	name, ok := procNames[pid]
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func hasKey(m map[string]float32, k string) bool {
	_, ok := m[k]
	return ok
}

// sessionPeak returns the session's instantaneous peak sample value (0..1)
// via IAudioMeterInformation, or 0 if the meter is unavailable. A stream
// that is open but carrying silence (a browser tab with an idle WebRTC
// pipeline) reads 0; speech or music reads well above it.
func sessionPeak(control *comObject) float32 {
	meter, ok := control.queryInterface(&iidIAudioMeterInformation)
	if !ok {
		return 0
	}
	defer meter.release()
	var peak float32
	if hr := meter.call(vtblMeterGetPeakValue, uintptr(unsafe.Pointer(&peak))); hr != 0 {
		return 0
	}
	return peak
}
