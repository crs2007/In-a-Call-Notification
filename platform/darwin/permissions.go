//go:build darwin

package darwin

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/crs2007/callmqtt/platform/darwin/internal/cf"
)

// Screen Recording access checks, available since macOS 10.15. Both return
// a C bool.
var (
	cgPreflightScreenCaptureAccess = coreGraphicsLib.Func("CGPreflightScreenCaptureAccess", cf.U8)
	cgRequestScreenCaptureAccess   = coreGraphicsLib.Func("CGRequestScreenCaptureAccess", cf.U8)
)

// ScreenRecordingGranted reports whether this process may read other
// applications' window titles (the Screen Recording privacy permission).
// It never prompts. If the check itself can't be made, it reports false:
// callers use this to decide whether to explain the missing permission,
// and "not granted" is the answer that leads to an explanation.
func ScreenRecordingGranted() bool {
	r, err := cgPreflightScreenCaptureAccess.Call()
	if err != nil {
		signalUnavailable("screen recording check (CGPreflightScreenCaptureAccess)", err)
		return false
	}
	return r&0xff != 0
}

// RequestScreenRecording asks macOS for the Screen Recording permission and
// reports whether it is granted right now.
//
// macOS shows its prompt at most once per app: after the user has answered
// (or dismissed) it, every call returns immediately without UI, and a
// denied app can only be enabled by hand in System Settings. Granting takes effect only after the app restarts — TCC does
// not hand a running process the new permission — so a false return right
// after the user clicks "Allow" is expected. Callers that want the user to
// change the setting by hand should open ScreenRecordingSettingsURL.
func RequestScreenRecording() bool {
	r, err := cgRequestScreenCaptureAccess.Call()
	if err != nil {
		signalUnavailable("screen recording request (CGRequestScreenCaptureAccess)", err)
		return false
	}
	return r&0xff != 0
}

// screenRecordingRecheck is how often VisibleWindows re-reads the Screen
// Recording permission. CGPreflightScreenCaptureAccess is a round trip to
// the TCC daemon costing several milliseconds — more than the whole rest of
// a poll — and the answer only matters for a log line, so it is sampled
// rather than read every poll.
const screenRecordingRecheck = time.Minute

var (
	// screenRecordingState is the last permission state
	// noteScreenRecordingState logged: 0 = never checked, 1 = granted,
	// 2 = not granted.
	screenRecordingState atomic.Int32
	// screenRecordingChecked is when the permission was last read, in
	// UnixNano; 0 means never.
	screenRecordingChecked atomic.Int64
)

// noteScreenRecordingState logs the Screen Recording permission state when
// it changes — on the first poll, and again only if the user grants or
// revokes it while the agent runs. Logging every poll would repeat the
// same line every two seconds for every user who declines the permission.
//
// At most one caller per screenRecordingRecheck interval actually reads the
// permission; concurrent pollers that lose the compare-and-swap skip it.
func noteScreenRecordingState() {
	now := time.Now().UnixNano()
	last := screenRecordingChecked.Load()
	if last != 0 && now-last < int64(screenRecordingRecheck) {
		return
	}
	if !screenRecordingChecked.CompareAndSwap(last, now) {
		return // another poller is checking right now
	}

	state := int32(2)
	if ScreenRecordingGranted() {
		state = 1
	}
	if screenRecordingState.Swap(state) == state {
		return
	}
	if state == 1 {
		slog.Info("screen recording permission granted; window titles available")
		return
	}
	slog.Info("screen recording permission not granted; window titles unavailable, "+
		"detecting calls from processes and microphone only",
		"settings", ScreenRecordingSettingsURL)
}
