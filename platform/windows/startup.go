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

// currentExe is the running executable with symlinks resolved.
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

// EnableStartup points HKCU's Run key at the current executable, quoted
// since install paths can contain spaces. A non-empty configPath is passed
// on as --config so the login launch uses the same config as this run.
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

	if err := k.SetStringValue(runValueName, runCommand(exe, configPath)); err != nil {
		return fmt.Errorf("write run value: %w", err)
	}
	return nil
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
// the current executable. A value for a moved or deleted install counts as
// not enabled, so re-enabling repairs it.
func StartupEnabled() (bool, error) {
	enabled, stale, _, err := StartupStatus()
	return enabled && !stale, err
}

// StartupStatus reports whether a run value exists (enabled), whether it
// points somewhere other than the current executable (stale), and the
// executable path it names (target).
func StartupStatus() (enabled, stale bool, target string, err error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, false, "", nil
		}
		return false, false, "", fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	v, _, err := k.GetStringValue(runValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, false, "", nil
		}
		return false, false, "", fmt.Errorf("read run value: %w", err)
	}
	exe, err := currentExe()
	if err != nil {
		return false, false, "", err
	}
	target, ok := parseRunCommand(v)
	return true, !ok || !sameExePath(target, exe), target, nil
}
