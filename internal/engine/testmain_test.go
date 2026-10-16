package engine

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// TestMain points the per-user config directory at a throwaway one for the
// whole package. Loading a config without an explicit mqtt.client_id creates
// the per-install id file (config.installSuffix), and without this every
// test run would write it into the developer's real %APPDATA%\callmqtt or
// ~/Library/Application Support/callmqtt.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "callmqtt-userconfig-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create temp user config dir:", err)
		os.Exit(1)
	}
	// Override only the variable os.UserConfigDir reads on this OS, so
	// anything else a test spawns (TestFirstLaunchPackagedGUI runs go build)
	// keeps its real caches and settings.
	switch runtime.GOOS {
	case "windows":
		os.Setenv("APPDATA", dir)
	case "darwin", "ios", "plan9":
		os.Setenv("HOME", dir)
	default:
		os.Setenv("XDG_CONFIG_HOME", dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
