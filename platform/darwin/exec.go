package darwin

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runFunc runs a command and returns its stdout and stderr. A command that
// ran but exited non-zero comes back as an *exitError, so callers can tell
// "no such keychain item" or "service not loaded" apart from a real failure.
//
// It is the one seam between this package's startup, dialog and keychain
// logic and the system tools they drive (launchctl, osascript, security):
// tests hand in a fake that records arguments and returns canned output, so
// the logic around those tools tests on every OS with nothing installed.
type runFunc func(name string, args ...string) (stdout, stderr []byte, err error)

// exitError is a command that ran and exited non-zero.
type exitError struct {
	name   string
	code   int
	output string // stderr, trimmed; stdout if stderr was empty
}

func (e *exitError) Error() string {
	if e.output == "" {
		return fmt.Sprintf("%s: exit status %d", e.name, e.code)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.name, e.code, e.output)
}

// exitCode reports the exit status of a command that ran and failed, and
// false for any other error (including nil).
func exitCode(err error) (int, bool) {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, true
	}
	return 0, false
}

// runCommand is the real runFunc. stdout and stderr are kept apart because
// `security -g` prints the secret on stderr and attributes on stdout; on
// failure, stderr is what explains why, so it also goes into the error.
func runCommand(name string, args ...string) (stdout, stderr []byte, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err = cmd.Run()
	stdout, stderr = outBuf.Bytes(), errBuf.Bytes()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		output := strings.TrimSpace(string(stderr))
		if output == "" {
			output = strings.TrimSpace(string(stdout))
		}
		return stdout, stderr, &exitError{name: name, code: ee.ExitCode(), output: output}
	}
	if err != nil {
		return stdout, stderr, fmt.Errorf("run %s: %w", name, err)
	}
	return stdout, stderr, nil
}
