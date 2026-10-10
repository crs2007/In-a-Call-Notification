//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestUserAgent pins the paths the exported Startup functions act on: the
// agreed plist location and the GUI login domain. The behaviour behind them
// is covered against a temporary directory by the launchAgent tests.
func TestUserAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a, err := userAgent()
	if err != nil {
		t.Fatalf("userAgent: %v", err)
	}
	checks := []struct{ name, got, want string }{
		{"plist", a.plistPath, filepath.Join(home, "Library", "LaunchAgents", "io.github.crs2007.callmqtt.plist")},
		{"domain", a.domain, "gui/" + strconv.Itoa(os.Getuid())},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	exe, err := a.exe()
	if err != nil || !filepath.IsAbs(exe) {
		t.Errorf("exe = %q, %v; want an absolute path", exe, err)
	}
}
