//go:build darwin

package darwin

import (
	"log/slog"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/crs2007/callmqtt/platform/darwin/internal/cf"
)

// procPidPath is libproc's proc_pidpath(pid, buffer, buffersize): it writes
// the full executable path of pid and returns its length, or <= 0 if the
// process is gone or not ours to inspect.
var procPidPath = libSystem.Func("proc_pidpath", cf.S32, cf.S32, cf.Pointer, cf.U32)

// procPidPathMax is PROC_PIDPATHINFO_MAXSIZE (4 * MAXPATHLEN).
const procPidPathMax = 4 * 1024

// ProcessNames maps every running process's PID to its executable base name
// (e.g. "MSTeams", "zoom.us", "Slack"). Callers pair this with
// VisibleWindows' PIDs and AppsUsingMicrophone's fallback naming.
//
// PIDs come from the kern.proc.all sysctl, which needs no permission.
// Names come from proc_pidpath, falling back to the kernel's truncated
// p_comm for the occasional process whose path can't be read (see
// processName). A process that exits mid-walk is simply skipped.
//
// A failed sysctl is not fatal: it means "no evidence" for this poll cycle,
// so this logs at slog.Debug and returns an empty (non-nil) map, exactly as
// platform/windows does when its Toolhelp32 snapshot fails. If libproc
// itself can't be loaded, every name comes from p_comm instead.
func ProcessNames() map[uint32]string {
	names := make(map[uint32]string)

	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		slog.Debug("list processes failed", "err", err)
		return names
	}

	pathOK := true
	if err := procPidPath.Load(); err != nil {
		signalUnavailable("process paths (proc_pidpath)", err)
		pathOK = false
	}

	// One buffer per call, not per process and not package-level: reusing
	// it across processes saves ~500 allocations a poll, and keeping it
	// local means concurrent callers never share it.
	buf := make([]byte, procPidPathMax)
	for i := range procs {
		pid := procs[i].Proc.P_pid
		if pid < 0 {
			continue
		}
		var path string
		if pathOK && pid > 0 {
			n, err := procPidPath.Call(uintptr(pid), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if l := int32(n); err == nil && l > 0 && int(l) <= len(buf) {
				path = cString(buf[:l])
			}
		}
		names[uint32(pid)] = processName(path, cString(procs[i].Proc.P_comm[:]))
	}
	return names
}
