package windows

import "strings"

// buildRunValue formats the HKCU Run value that launches callmqtt at login.
// It uses Windows command-line quoting (wrap in literal double quotes), not
// Go's %q string-literal escaping, which doubles backslashes and produces a
// value that Windows only happens to accept because path normalisation
// collapses the repeated separators.
//
// --config is appended only when configPath differs from defaultConfigPath,
// so a user running with the default config gets a plain, minimal Run value
// and a user running with an override keeps that override across logins.
func buildRunValue(exe, configPath, defaultConfigPath string) string {
	if configPath == defaultConfigPath {
		return `"` + exe + `"`
	}
	return `"` + exe + `" --config "` + configPath + `"`
}

// runValueExe extracts the executable path from a Run value built by
// buildRunValue: the first double-quoted segment. ok is false for an empty
// or malformed value (no opening/closing quote).
//
// This is pure string parsing with no path/filepath dependency, deliberately
// — it must parse Windows paths correctly even when this file's tests run on
// a non-Windows GOOS.
func runValueExe(value string) (exe string, ok bool) {
	if len(value) == 0 || value[0] != '"' {
		return "", false
	}
	end := strings.IndexByte(value[1:], '"')
	if end < 0 {
		return "", false
	}
	return value[1 : 1+end], true
}

// runValueMatchesExe reports whether a stored Run value still points at exe.
// Windows paths are case-insensitive, so the comparison folds case.
func runValueMatchesExe(value, exe string) bool {
	storedExe, ok := runValueExe(value)
	if !ok {
		return false
	}
	return strings.EqualFold(storedExe, exe)
}
