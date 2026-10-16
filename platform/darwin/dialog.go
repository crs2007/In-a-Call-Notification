//go:build darwin

package darwin

// osascript is invoked by absolute path, like launchctl and security.
const osascript = "/usr/bin/osascript"

// ShowError pops a blocking alert with a stop icon. It exists for the same
// reason as platform/windows's ShowError: an app opened from Finder or
// started at login has stderr on /dev/null, so a startup error — most
// visibly "no config yet" on first launch — is otherwise invisible and the
// process just exits with nothing on screen.
func ShowError(title, message string) {
	showAlert(runCommand, "critical", title, message)
}

// ShowInfo is ShowError with an informational icon, for a message that is
// not a failure — "wrote your starter config here" — but still has nowhere
// else to go.
func ShowInfo(title, message string) {
	showAlert(runCommand, "informational", title, message)
}

// showAlert runs the alert and waits for it to be dismissed. Failure is
// ignored: there is no further fallback, and the caller already wrote the
// same text to stderr.
func showAlert(run runFunc, style, title, message string) {
	_, _, _ = run(osascript, alertArgs(style, title, message)...)
}

// alertArgs is osascript's command line. The title and message are read
// from argv rather than spliced into the AppleScript source, so no quote,
// backslash or `& "..."` in an error message (which can echo a user's file
// path) can break out of the string and run as script. "--" ends option
// parsing, so text starting with "-" is an argument, not a flag. style is
// fixed by this package, never user text.
func alertArgs(style, title, message string) []string {
	return []string{
		"-e", "on run argv",
		"-e", `display alert (item 1 of argv) message (item 2 of argv) as ` + style + ` buttons {"OK"} default button "OK"`,
		"-e", "end run",
		"--", title, message,
	}
}
