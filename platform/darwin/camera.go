//go:build darwin

package darwin

import (
	"log/slog"
	"unsafe"

	"github.com/crs2007/callmqtt/platform/darwin/internal/cf"
)

// CoreMediaIO property access. Note the shape differs from Core Audio's:
// CMIOObjectGetPropertyData takes the buffer size BY VALUE, then a separate
// dataUsed out-pointer, then the buffer — seven arguments, not six. Calling
// it with Core Audio's six-argument shape crashes the process, which is why
// each is declared separately rather than shared.
var (
	cmioObjectGetPropertyDataSize = coreMediaIOLib.Func("CMIOObjectGetPropertyDataSize", cf.S32, cf.U32, cf.Pointer, cf.U32, cf.Pointer, cf.Pointer)
	cmioObjectGetPropertyData     = coreMediaIOLib.Func("CMIOObjectGetPropertyData", cf.S32, cf.U32, cf.Pointer, cf.U32, cf.Pointer, cf.U32, cf.Pointer, cf.Pointer)
)

// cmioUint32 reads a 4-byte CoreMediaIO property.
func cmioUint32(obj, sel uint32) (uint32, error) {
	addr := globalProperty(sel)
	var v, used uint32
	st, err := cmioObjectGetPropertyData.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(unsafe.Sizeof(v)), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&v)))
	if err != nil {
		return 0, err
	}
	if st := int32(st); st != 0 {
		return 0, osStatusError(st)
	}
	return v, nil
}

// cmioObjectList reads an array-of-CMIOObjectID property, with the same
// grow-between-calls headroom as audioObjectList.
func cmioObjectList(obj, sel uint32) ([]uint32, error) {
	addr := globalProperty(sel)
	var size uint32
	st, err := cmioObjectGetPropertyDataSize.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
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
	var used uint32
	st, err = cmioObjectGetPropertyData.Call(uintptr(obj), uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(len(ids)*4), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&ids[0])))
	if err != nil {
		return nil, err
	}
	if st := int32(st); st != 0 {
		return nil, osStatusError(st)
	}
	return ids[:min(int(used/4), len(ids))], nil
}

// AppsUsingWebcam reports whether any camera is in use, as ["(unknown)"],
// or nil when none is.
//
// This is device-level only: CoreMediaIO's
// kCMIODevicePropertyDeviceIsRunningSomewhere says a device is streaming to
// *some* process, and macOS has no public, permission-free API that names
// which one. That is an accepted gap — no shipped rule weights camera
// evidence (see rules.go Weights.Cam) — and reporting "(unknown)" keeps the
// signal honest rather than guessing an owner. procNames is accepted for
// signature parity with AppsUsingMicrophone and is unused.
//
// Every failure means "no evidence": nil, never an error.
func AppsUsingWebcam(map[uint32]string) []string {
	for _, f := range []*cf.Func{cmioObjectGetPropertyDataSize, cmioObjectGetPropertyData} {
		if err := f.Load(); err != nil {
			signalUnavailable("camera (CoreMediaIO)", err)
			return nil
		}
	}

	devices, err := cmioObjectList(systemObject, selCMIODevices)
	if err != nil {
		slog.Debug("read CoreMediaIO device list failed", "err", err)
		return nil
	}
	for _, dev := range devices {
		running, err := cmioUint32(dev, selDeviceIsRunningSomewhere)
		if err != nil {
			slog.Debug("read camera running state failed", "device", dev, "err", err)
			continue
		}
		if running != 0 {
			return deviceLevelUsers(true)
		}
	}
	return nil
}
