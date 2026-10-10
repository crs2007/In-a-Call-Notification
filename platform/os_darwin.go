//go:build darwin

package platform

import platformdarwin "github.com/crs2007/callmqtt/platform/darwin"

// VisibleWindows reports every on-screen, normal-layer window.
func VisibleWindows() []WindowInfo {
	wins := platformdarwin.VisibleWindows()
	out := make([]WindowInfo, len(wins))
	for i, w := range wins {
		out[i] = WindowInfo(w)
	}
	return out
}

// ProcessNames maps every running process's PID to its executable name.
func ProcessNames() map[uint32]string { return platformdarwin.ProcessNames() }

// AppsUsingMicrophone reports the applications holding the microphone right
// now. procNames is ProcessNames' result from the same poll.
func AppsUsingMicrophone(procNames map[uint32]string) []string {
	return platformdarwin.AppsUsingMicrophone(procNames)
}

// AppsUsingWebcam reports the applications holding the camera right now.
func AppsUsingWebcam(procNames map[uint32]string) []string {
	return platformdarwin.AppsUsingWebcam(procNames)
}
