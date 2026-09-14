//go:build windows

package windows

import (
	"log/slog"

	"github.com/shirou/gopsutil/v4/process"
)

// ProcessNames maps every running process's PID to its executable name.
// Callers pair this with VisibleWindows' PIDs to label window titles.
//
// A process that disappears mid-enumeration, or whose name can't be read
// (e.g. a protected system process), is skipped rather than treated as a
// failure.
func ProcessNames() map[uint32]string {
	names := make(map[uint32]string)
	procs, err := process.Processes()
	if err != nil {
		slog.Debug("enumerate processes failed", "err", err)
		return names
	}
	for _, p := range procs {
		if name, err := p.Name(); err == nil {
			names[uint32(p.Pid)] = name
		}
	}
	return names
}
