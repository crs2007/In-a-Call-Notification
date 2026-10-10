package darwin

import "strings"

// processName picks the name ProcessNames reports for one process: the base
// name of its executable path when proc_pidpath produced one, else the
// kernel's p_comm.
//
// The path is preferred because p_comm is truncated to MAXCOMLEN (16)
// bytes, which would turn "Microsoft Teams WebView Helper" into an
// unmatchable fragment. p_comm still covers the few processes whose path
// the caller may not read (e.g. some launchd-owned daemons).
//
// The split is done by hand on '/' rather than with path/filepath so the
// result is identical when this file's tests run on Windows.
func processName(path, comm string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	if path != "" {
		return path
	}
	return comm
}

// cString trims a fixed-size, NUL-terminated byte buffer (such as
// kinfo_proc's P_comm, or proc_pidpath's output) at its first NUL.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
