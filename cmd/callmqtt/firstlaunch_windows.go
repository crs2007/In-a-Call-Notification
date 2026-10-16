//go:build windows

package main

// consoleAttached is always true on Windows, which leaves main's rule as it
// was: a dialog only when writing the message failed. The shipped
// -H=windowsgui build launched from Explorer has no std handles at all, so
// that write fails exactly when nobody could see it, while a redirect to NUL
// or a file is a choice the user made.
func consoleAttached() bool { return true }

// passwordAdvice is the password paragraph of `callmqtt init`'s message.
const passwordAdvice = `The password is read from the CALLMQTT_MQTT_PASSWORD environment variable
(config.yaml only references it as ${CALLMQTT_MQTT_PASSWORD}). Set it with:
  setx CALLMQTT_MQTT_PASSWORD "yourpassword"
then open a new terminal, since setx does not affect the current one.
`

// literalPasswordAdvice completes the warning logged for a password written
// directly into the config file.
const literalPasswordAdvice = "prefer ${ENV_VAR}"
