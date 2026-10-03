package windows

import "strings"

// runCommand builds the HKCU Run value: the quoted executable, plus
// --config "<path>" when cfgPath is non-empty. Windows command lines use
// plain double quotes, not Go string-literal escaping, and paths cannot
// contain a double quote, so no further escaping is needed.
//
// Pure string work with no registry dependency, so it tests on every platform.
func runCommand(exe, cfgPath string) string {
	cmd := `"` + exe + `"`
	if cfgPath != "" {
		cmd += ` --config "` + cfgPath + `"`
	}
	return cmd
}

// parseRunCommand extracts the executable path from a stored Run value. It
// accepts a quoted first token (what runCommand writes) and falls back to an
// unquoted one. ok is false for an empty or malformed value.
func parseRunCommand(v string) (exe string, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	if v[0] == '"' {
		end := strings.IndexByte(v[1:], '"')
		if end < 0 {
			return "", false
		}
		exe = v[1 : 1+end]
	} else if i := strings.IndexAny(v, " \t"); i >= 0 {
		exe = v[:i]
	} else {
		exe = v
	}
	return exe, exe != ""
}

// sameExePath reports whether a stored path and the current executable name
// the same file. Doubled separators, as older releases wrote with %q, are
// collapsed, and the comparison is case-insensitive as Windows paths are.
func sameExePath(stored, current string) bool {
	return strings.EqualFold(normalizeWinPath(stored), normalizeWinPath(current))
}

// normalizeWinPath collapses repeated backslashes (keeping a leading UNC
// pair) without filepath.Clean, so the result is the same on every platform.
func normalizeWinPath(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	unc := strings.HasPrefix(p, `\\`)
	for strings.Contains(p, `\\`) {
		p = strings.ReplaceAll(p, `\\`, `\`)
	}
	if unc {
		p = `\` + p
	}
	return p
}
