//go:build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// runKey is HKCU, not HKLM: launching at login this way needs no admin
// rights, and the run value only ever affects the current user anyway.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// runValueName is both the registry value name and how the entry appears in
// Task Manager's Startup tab.
const runValueName = "CallMQTT"

// EnableStartup points HKCU's Run key at the current executable, quoted the
// way the Windows shell expects rather than Go's %q string-literal escaping
// (which doubles backslashes and does not launch reliably). configPath is
// appended as a quoted --config argument when the running instance used a
// non-default config path; pass "" to launch with no arguments.
func EnableStartup(configPath string) error {
	exe, err := currentExe()
	if err != nil {
		return err
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(runValueName, formatRunValue(exe, configPath)); err != nil {
		return fmt.Errorf("write run value: %w", err)
	}
	return nil
}

// currentExe resolves the running executable's path, following symlinks, so
// EnableStartup and StartupEnabled compare against the same canonical path.
func currentExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// DisableStartup removes the run value. Removing one that is already absent
// is not an error, so callers can call it unconditionally.
func DisableStartup() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("delete run value: %w", err)
	}
	return nil
}

// StartupEnabled reports whether the run value is set and still points at
// this executable. A stored value pointing at a different (e.g. moved or
// re-extracted) path is reported as disabled, so re-ticking the tray
// checkbox repairs it rather than leaving a dead entry that looks enabled.
func StartupEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	stored, _, err := k.GetStringValue(runValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("read run value: %w", err)
	}

	exe, err := currentExe()
	if err != nil {
		return false, err
	}
	return runValueMatchesExe(stored, exe), nil
}
