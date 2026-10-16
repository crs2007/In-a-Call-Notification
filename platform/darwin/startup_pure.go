package darwin

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// LaunchAgentLabel names the login item to launchd, and is the plist's file
// name under ~/Library/LaunchAgents. Reverse-DNS of the GitHub repository, so
// it cannot collide with another vendor's agent.
const LaunchAgentLabel = "io.github.crs2007.callmqtt"

// launchctl is invoked by absolute path so a PATH-less login session, or a
// shadowing binary earlier on PATH, cannot change what runs.
const launchctl = "/bin/launchctl"

// renderPlist builds the LaunchAgent property list: exe, plus --config
// cfgPath when cfgPath is non-empty, started once at login (RunAtLoad), never
// restarted after the user quits it (KeepAlive false), and scheduled as a
// foreground app rather than a throttled background daemon (Interactive).
//
// Paths are XML-escaped, so &, < and quotes in a folder name cannot break the
// document. A path holding a character XML cannot carry at all (most control
// characters, invalid UTF-8) is refused rather than silently altered, since
// the altered path would launch nothing at login.
func renderPlist(exe, cfgPath string) ([]byte, error) {
	args := []string{exe}
	if cfgPath != "" {
		args = append(args, "--config", cfgPath)
	}

	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + LaunchAgentLabel + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range args {
		if err := checkXMLText(arg); err != nil {
			return nil, fmt.Errorf("launch agent argument %q: %w", arg, err)
		}
		b.WriteString("\t\t<string>")
		if err := xml.EscapeText(&b, []byte(arg)); err != nil {
			return nil, fmt.Errorf("escape launch agent argument: %w", err)
		}
		b.WriteString("</string>\n")
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<false/>\n")
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Interactive</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes(), nil
}

// checkXMLText rejects text that XML 1.0 cannot represent even escaped.
func checkXMLText(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("contains invalid UTF-8")
	}
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("contains control character U+%04X", r)
		}
	}
	return nil
}

// plistDoc is just enough of a property list to find what it launches. Dict
// entries are a flat key, value, key, value sequence, so they are read in
// order rather than mapped to fields.
type plistDoc struct {
	Dict struct {
		Items []struct {
			XMLName xml.Name
			Text    string   `xml:",chardata"`
			Strings []string `xml:"string"`
		} `xml:",any"`
	} `xml:"dict"`
}

// parsePlistProgram extracts the executable a LaunchAgent plist launches:
// Program if present (launchd prefers it), otherwise ProgramArguments[0].
// ok is false for a plist this cannot read, including a binary plist a user
// converted by hand; Status then reports it stale and `enable` rewrites it.
func parsePlistProgram(data []byte) (exe string, ok bool) {
	var doc plistDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", false
	}
	items := doc.Dict.Items
	var args []string
	for i := 0; i+1 < len(items); i += 2 {
		if items[i].XMLName.Local != "key" {
			return "", false
		}
		value := items[i+1]
		switch strings.TrimSpace(items[i].Text) {
		case "Program":
			if value.XMLName.Local == "string" && value.Text != "" {
				return value.Text, true
			}
		case "ProgramArguments":
			if value.XMLName.Local == "array" {
				args = value.Strings
			}
		}
	}
	if len(args) == 0 || args[0] == "" {
		return "", false
	}
	return args[0], true
}

// sameExePath reports whether a plist's executable and the running one are
// the same file, resolving symlinks on both sides so a Homebrew-style
// symlinked install is not reported stale. A path that no longer resolves
// (the install was moved or deleted) is compared as written.
func sameExePath(stored, current string) bool {
	return resolvePath(stored) == resolvePath(current)
}

func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// launchAgent is everything enable, disable and status touch, injected so
// tests can point it at a temporary directory and a fake launchctl.
type launchAgent struct {
	plistPath string                 // ~/Library/LaunchAgents/<label>.plist
	domain    string                 // gui/<uid>
	exe       func() (string, error) // the running executable, symlinks resolved
	run       runFunc
}

// enable writes the plist and, if load is set, swaps it into launchd now:
// any loaded copy is booted out first (bootstrap refuses to load a label
// twice), then the new one is bootstrapped. Without load the plist is only
// written, and launchd picks it up at the next login.
func (a launchAgent) enable(cfgPath string, load bool) error {
	exe, err := a.exe()
	if err != nil {
		return err
	}
	// Gatekeeper runs a quarantined app opened from Downloads out of a
	// random read-only copy that is gone after a restart, so a login item
	// pointing there would silently launch nothing.
	if strings.Contains(exe, "/AppTranslocation/") {
		return fmt.Errorf("%s is a temporary copy macOS made because the app was opened from where it was downloaded; move it to /Applications, open it from there, and enable start at login again", exe)
	}
	body, err := renderPlist(exe, cfgPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.plistPath), 0o755); err != nil {
		return fmt.Errorf("create launch agents directory: %w", err)
	}
	if err := os.WriteFile(a.plistPath, body, 0o644); err != nil {
		return fmt.Errorf("write launch agent: %w", err)
	}
	// WriteFile's mode applies only to a new file; launchd ignores a plist
	// that is group- or world-writable, so an old file's mode is fixed too.
	if err := os.Chmod(a.plistPath, 0o644); err != nil {
		return fmt.Errorf("set launch agent permissions: %w", err)
	}
	if !load {
		return nil
	}
	if err := a.bootout(); err != nil {
		return err
	}
	if _, _, err := a.run(launchctl, "bootstrap", a.domain, a.plistPath); err != nil {
		return fmt.Errorf("load launch agent: %w", err)
	}
	return nil
}

// disable removes the plist and, if unload is set, boots the job out of
// launchd, which also stops a login-launched copy that is still running.
// Both steps tolerate "already gone", so disable is idempotent.
func (a launchAgent) disable(unload bool) error {
	if unload {
		if err := a.bootout(); err != nil {
			return err
		}
	}
	if err := os.Remove(a.plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove launch agent: %w", err)
	}
	return nil
}

// bootout unloads the job, treating "not loaded" as success. launchctl
// reports that as exit 3 ("No such process") or, on some releases, 113
// ("Could not find service").
func (a launchAgent) bootout() error {
	_, _, err := a.run(launchctl, "bootout", a.domain+"/"+LaunchAgentLabel)
	if code, ok := exitCode(err); ok && (code == 3 || code == 113) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unload launch agent: %w", err)
	}
	return nil
}

// status reports whether a plist exists (enabled), whether it launches
// something other than the running executable (stale), and what it launches
// (target), with the same meaning as platform/windows's StartupStatus.
func (a launchAgent) status() (enabled, stale bool, target string, err error) {
	data, err := os.ReadFile(a.plistPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, "", nil
	}
	if err != nil {
		return false, false, "", fmt.Errorf("read launch agent: %w", err)
	}
	exe, err := a.exe()
	if err != nil {
		return false, false, "", err
	}
	target, ok := parsePlistProgram(data)
	return true, !ok || !sameExePath(target, exe), target, nil
}
