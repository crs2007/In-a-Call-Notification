//go:build darwin

package darwin

import (
	"fmt"
	"log/slog"
	"unsafe"

	"github.com/crs2007/callmqtt/platform/darwin/internal/cf"
)

// Core Audio HAL property access. The address argument points at an
// AudioObjectPropertyAddress {mSelector, mScope, mElement uint32}; the
// qualifier arguments are always 0/NULL here. GetPropertyData's ioDataSize
// is in/out: the buffer size going in, the bytes written coming out.
var (
	audioObjectHasProperty         = coreAudioLib.Func("AudioObjectHasProperty", cf.U8, cf.U32, cf.Pointer)
	audioObjectGetPropertyDataSize = coreAudioLib.Func("AudioObjectGetPropertyDataSize", cf.S32, cf.U32, cf.Pointer, cf.U32, cf.Pointer, cf.Pointer)
	audioObjectGetPropertyData     = coreAudioLib.Func("AudioObjectGetPropertyData", cf.S32, cf.U32, cf.Pointer, cf.U32, cf.Pointer, cf.Pointer, cf.Pointer)
)

// propertyAddress is AudioObjectPropertyAddress, and also
// CMIOObjectPropertyAddress, which has the same layout.
type propertyAddress struct {
	selector, scope, element uint32
}

func globalProperty(selector uint32) propertyAddress {
	return propertyAddress{selector: selector, scope: scopeGlobal, element: elementMain}
}

// osStatusError is a non-zero OSStatus from a Core Audio or CoreMediaIO
// call, printed both as a number and — when it is one — as the
// four-character code Apple's headers spell it with (e.g. 'who?').
type osStatusError int32

func (e osStatusError) Error() string {
	u := uint32(e)
	b := []byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return fmt.Sprintf("OSStatus %d", int32(e))
		}
	}
	return fmt.Sprintf("OSStatus %d ('%s')", int32(e), b)
}

// audioHasProperty reports whether obj implements the global property sel.
func audioHasProperty(obj, sel uint32) bool {
	addr := globalProperty(sel)
	r, err := audioObjectHasProperty.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)))
	return err == nil && r&0xff != 0
}

// audioUint32 reads a 4-byte property: a UInt32 flag, an AudioObjectID or a
// pid_t (reinterpret with int32()).
func audioUint32(obj, sel uint32) (uint32, error) {
	addr := globalProperty(sel)
	var v uint32
	size := uint32(unsafe.Sizeof(v))
	st, err := audioObjectGetPropertyData.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&v)))
	if err != nil {
		return 0, err
	}
	if st := int32(st); st != 0 {
		return 0, osStatusError(st)
	}
	if size != uint32(unsafe.Sizeof(v)) {
		return 0, fmt.Errorf("property %#x: got %d bytes, want 4", sel, size)
	}
	return v, nil
}

// audioCFString reads a CFStringRef-valued property. Core Audio hands back
// a retained reference, so it is converted and released here.
func audioCFString(obj, sel uint32) (string, error) {
	addr := globalProperty(sel)
	var ref uintptr
	size := uint32(unsafe.Sizeof(ref))
	st, err := audioObjectGetPropertyData.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&ref)))
	if err != nil {
		return "", err
	}
	if st := int32(st); st != 0 {
		return "", osStatusError(st)
	}
	defer cf.Release(cf.Ref(ref))
	if size != uint32(unsafe.Sizeof(ref)) {
		return "", fmt.Errorf("property %#x: got %d bytes, want %d", sel, size, unsafe.Sizeof(ref))
	}
	return cf.String(cf.Ref(ref)), nil
}

// audioObjectList reads an array-of-AudioObjectID property. The list can
// grow between the size query and the read (a process starting audio), so
// the buffer gets a little headroom and the result is trimmed to what Core
// Audio actually wrote.
func audioObjectList(obj, sel uint32) ([]uint32, error) {
	addr := globalProperty(sel)
	var size uint32
	st, err := audioObjectGetPropertyDataSize.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(unsafe.Pointer(&size)))
	if err != nil {
		return nil, err
	}
	if st := int32(st); st != 0 {
		return nil, osStatusError(st)
	}
	if size == 0 {
		return nil, nil
	}
	ids := make([]uint32, size/4+8)
	size = uint32(len(ids) * 4)
	st, err = audioObjectGetPropertyData.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&ids[0])))
	if err != nil {
		return nil, err
	}
	if st := int32(st); st != 0 {
		return nil, osStatusError(st)
	}
	return ids[:min(int(size/4), len(ids))], nil
}

// audioReady loads every Core Audio function AppsUsingMicrophone calls, so
// a missing framework is one log line rather than an error per property.
func audioReady() error {
	for _, f := range []*cf.Func{audioObjectHasProperty, audioObjectGetPropertyDataSize, audioObjectGetPropertyData} {
		if err := f.Load(); err != nil {
			return err
		}
	}
	return cf.Ready()
}

// AppsUsingMicrophone reports the applications capturing audio input right
// now, as sorted bundle IDs (e.g. "com.microsoft.teams2", "us.zoom.xos").
//
// On macOS 14.2 and later Core Audio exposes one object per audio client
// process (kAudioHardwarePropertyProcessObjectList) with its PID, bundle ID
// and whether it is running input. That is real per-app attribution with no
// permission prompt — and unlike Windows' ConsentStore it separates input
// from output, so an app merely playing music does not count. Naming rules
// (bundle ID, then procNames[pid], then "(unknown)") are microphoneUsers'.
//
// Before 14.2 there is no per-process list; the best Core Audio offers is
// whether the default input device is running somewhere, reported as
// ["(unknown)"] so rules can give it a deliberately low weight.
//
// procNames is ProcessNames' result from the same poll, used only for the
// name fallback. Every failure means "no evidence": nil, never an error.
func AppsUsingMicrophone(procNames map[uint32]string) []string {
	if err := audioReady(); err != nil {
		signalUnavailable("microphone (CoreAudio)", err)
		return nil
	}

	if !audioHasProperty(systemObject, selProcessObjectList) {
		return defaultInputDeviceUsers()
	}

	objs, err := audioObjectList(systemObject, selProcessObjectList)
	if err != nil {
		slog.Debug("read Core Audio process list failed", "err", err)
		return nil
	}
	procs := make([]audioProcess, 0, len(objs))
	for _, obj := range objs {
		running, err := audioUint32(obj, selProcessIsRunningInput)
		if err != nil || running == 0 {
			continue // gone mid-walk, or not capturing: either way not evidence
		}
		p := audioProcess{inputRunning: true}
		if pid, err := audioUint32(obj, selProcessPID); err == nil {
			p.pid = int32(pid)
		}
		if bundle, err := audioCFString(obj, selProcessBundleID); err == nil {
			p.bundleID = bundle
		}
		procs = append(procs, p)
	}
	return microphoneUsers(procs, procNames)
}

// defaultInputDeviceUsers is the pre-14.2 fallback: device-level evidence
// from the default input device's "running somewhere" flag.
func defaultInputDeviceUsers() []string {
	dev, err := audioUint32(systemObject, selDefaultInputDevice)
	if err != nil || dev == 0 { // 0 is kAudioObjectUnknown: no input device at all
		slog.Debug("no default input device", "err", err)
		return nil
	}
	running, err := audioUint32(dev, selDeviceIsRunningSomewhere)
	if err != nil {
		slog.Debug("read input device running state failed", "err", err)
		return nil
	}
	return deviceLevelUsers(running != 0)
}
