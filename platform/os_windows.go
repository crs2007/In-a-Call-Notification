//go:build windows

package platform

import platformwindows "github.com/crs2007/callmqtt/platform/windows"

// VisibleWindows reports every visible, titled top-level window.
func VisibleWindows() []WindowInfo {
	wins := platformwindows.VisibleWindows()
	out := make([]WindowInfo, len(wins))
	for i, w := range wins {
		out[i] = WindowInfo(w)
	}
	return out
}

// ProcessNames maps every running process's PID to its executable name.
func ProcessNames() map[uint32]string { return platformwindows.ProcessNames() }

// AppsUsingMicrophone reports the applications holding the microphone right
// now. procNames is ProcessNames' result from the same poll.
func AppsUsingMicrophone(procNames map[uint32]string) []string {
	return platformwindows.AppsUsingMicrophone(procNames)
}

// AppsUsingWebcam reports the applications holding the camera right now.
func AppsUsingWebcam(procNames map[uint32]string) []string {
	return platformwindows.AppsUsingWebcam(procNames)
}
