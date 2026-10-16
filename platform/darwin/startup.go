//go:build darwin

package darwin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

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

// userAgent is the per-user LaunchAgent in the GUI login session: no admin
// rights needed, and it only ever starts the agent for this user.
func userAgent() (launchAgent, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return launchAgent{}, fmt.Errorf("locate home directory: %w", err)
	}
	return launchAgent{
		plistPath: filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist"),
		domain:    "gui/" + strconv.Itoa(os.Getuid()),
		exe:       currentExe,
		run:       runCommand,
	}, nil
}

// EnableStartup writes ~/Library/LaunchAgents/<LaunchAgentLabel>.plist
// pointing at the current executable, with --config configPath when that is
// non-empty, so the login launch uses the same config as this run.
//
// With load set it also loads the job into launchd now, replacing any loaded
// copy. RunAtLoad means that starts an instance immediately — right for the
// CLI, wrong for an agent that is already running (it would start a second
// copy, or boot out itself), which passes load=false and lets the next login
// pick the plist up.
func EnableStartup(configPath string, load bool) error {
	a, err := userAgent()
	if err != nil {
		return err
	}
	return a.enable(configPath, load)
}

// DisableStartup removes the plist; with unload set it also boots the job
// out of launchd, stopping a login-launched copy. Calling it when nothing is
// installed is not an error.
func DisableStartup(unload bool) error {
	a, err := userAgent()
	if err != nil {
		return err
	}
	return a.disable(unload)
}

// StartupEnabled reports whether the plist exists and still points at the
// current executable. One for a moved or deleted install counts as not
// enabled, so re-enabling repairs it.
func StartupEnabled() (bool, error) {
	enabled, stale, _, err := StartupStatus()
	return enabled && !stale, err
}

// StartupStatus reports whether the plist exists (enabled), whether it
// launches something other than the current executable (stale), and the
// executable path it names (target).
func StartupStatus() (enabled, stale bool, target string, err error) {
	a, err := userAgent()
	if err != nil {
		return false, false, "", err
	}
	return a.status()
}
