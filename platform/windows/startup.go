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

// EnableStartup points HKCU's Run key at the current executable, quoted
// since install paths can contain spaces.
func EnableStartup() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(runValueName, fmt.Sprintf("%q", exe)); err != nil {
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

// StartupEnabled reports whether the run value is currently set.
func StartupEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	if _, _, err := k.GetStringValue(runValueName); err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("read run value: %w", err)
	}
	return true, nil
}
