//go:build darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// consoleAttached reports whether any of stdin, stdout or stderr is a
// terminal. An app opened from Finder or started at login has all three on
// /dev/null, where a write succeeds and is seen by nobody, so the Windows
// rule (dialog only when the write fails) would show nothing. Checking all
// three rather than just stderr keeps `callmqtt init > file` or a
// `2>/dev/null` typed in Terminal from popping an alert.
func consoleAttached() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if isTerminal(f.Fd()) {
			return true
		}
	}
	return false
}

// isTerminal is isatty(3): only a terminal answers the termios ioctl.
func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TIOCGETA)
	return err == nil
}

// passwordAdvice is the password paragraph of `callmqtt init`'s message.
// The ${CALLMQTT_MQTT_PASSWORD} advice Windows gets does not work here:
// launchd and Finder never read a shell profile, so the variable reaches the
// app empty.
const passwordAdvice = `Keep the password in your login keychain. Apps started from Finder or at
login do not see shell environment variables, so the starter config's
${CALLMQTT_MQTT_PASSWORD} would arrive empty. Instead set:
  mqtt.password      "keychain:callmqtt"
and store the password (it prompts for it) with:
  security add-generic-password -U -s callmqtt -a <your mqtt.username> -w
`

// literalPasswordAdvice completes the warning logged for a password written
// directly into the config file.
const literalPasswordAdvice = `prefer "keychain:callmqtt" (see callmqtt init)`
