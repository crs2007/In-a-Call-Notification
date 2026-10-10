//go:build !windows && !darwin

package main

// consoleAttached is always true here: there is no dialog to fall back to
// (see dialog_stub.go), so main only reports what failed to print.
func consoleAttached() bool { return true }

// passwordAdvice is the password paragraph of `callmqtt init`'s message.
const passwordAdvice = `The password is read from the CALLMQTT_MQTT_PASSWORD environment variable
(config.yaml only references it as ${CALLMQTT_MQTT_PASSWORD}). Set it in the
environment the agent starts from, for example:
  export CALLMQTT_MQTT_PASSWORD="yourpassword"
`

// literalPasswordAdvice completes the warning logged for a password written
// directly into the config file.
const literalPasswordAdvice = "prefer ${ENV_VAR}"
