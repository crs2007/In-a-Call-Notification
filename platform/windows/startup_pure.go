package windows

import "strings"

// formatRunValue builds the HKCU Run value that launches exe at login,
// quoted the way the Windows shell (not Go's %q string-literal escaping)
// expects: a quoted path, with a quoted --config argument appended only when
// the running instance used a non-default config path.
//
// This is pure string formatting with no registry dependency, so it builds
// and tests on every platform.
func formatRunValue(exe, configPath string) string {
	value := `"` + exe + `"`
	if configPath != "" {
		value += ` --config "` + configPath + `"`
	}
	return value
}

// parseRunValueExe extracts the quoted exe path formatRunValue put first in
// a Run value. It reports ok=false for a value that does not start with a
// quoted token, which StartupEnabled treats as "not something we wrote" and
// so not confidently comparable.
func parseRunValueExe(value string) (exe string, ok bool) {
	if len(value) == 0 || value[0] != '"' {
		return "", false
	}
	end := strings.IndexByte(value[1:], '"')
	if end < 0 {
		return "", false
	}
	return value[1 : 1+end], true
}

// runValueMatchesExe reports whether a stored Run value points at exe.
// Windows paths are case-insensitive, so the comparison is too.
func runValueMatchesExe(storedValue, exe string) bool {
	storedExe, ok := parseRunValueExe(storedValue)
	if !ok {
		return false
	}
	return strings.EqualFold(storedExe, exe)
}
