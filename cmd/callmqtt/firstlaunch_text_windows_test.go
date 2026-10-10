//go:build windows

package main

import "testing"

// TestInitMessageWindowsUnchanged pins the Windows `callmqtt init` text to
// what it was before the message became per-OS, byte for byte.
func TestInitMessageWindowsUnchanged(t *testing.T) {
	const want = `Wrote C:\Users\s\AppData\Roaming\callmqtt\config.yaml

This machine will appear as device id "desk", so its topics are:
  desktop-presence/desk/call
  desktop-presence/desk/availability

Before starting, edit that file and set:
  mqtt.host          your broker's address
  mqtt.username      if your broker requires one
  allowed_networks   the subnets you are willing to publish from

The password is read from the CALLMQTT_MQTT_PASSWORD environment variable
(config.yaml only references it as ${CALLMQTT_MQTT_PASSWORD}). Set it with:
  setx CALLMQTT_MQTT_PASSWORD "yourpassword"
then open a new terminal, since setx does not affect the current one.
Then check your work with:  callmqtt --validate-config
`
	if got := initMessage(`C:\Users\s\AppData\Roaming\callmqtt\config.yaml`, "desk"); got != want {
		t.Errorf("init message changed:\n got: %q\nwant: %q", got, want)
	}
	if !consoleAttached() {
		t.Error("consoleAttached must stay true on Windows: dialogs pop only when the write fails")
	}
}
