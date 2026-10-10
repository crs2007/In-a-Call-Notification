//go:build darwin

package darwin

// WindowInfo mirrors platform.WindowInfo so platform/os_darwin.go can
// convert between the two without this package importing its caller.
type WindowInfo struct {
	PID   uint32
	Title string
}

// VisibleWindows reports every on-screen, normal-layer window with its
// owning PID and title. Not implemented yet: returns no evidence.
func VisibleWindows() []WindowInfo { return nil }

// ProcessNames maps every running process's PID to its executable base
// name. Not implemented yet: returns no evidence.
func ProcessNames() map[uint32]string { return map[uint32]string{} }

// AppsUsingMicrophone reports the bundle IDs of applications currently
// capturing audio input. Not implemented yet: returns no evidence.
func AppsUsingMicrophone(map[uint32]string) []string { return nil }

// AppsUsingWebcam reports applications holding the camera. Not implemented
// yet: returns no evidence.
func AppsUsingWebcam(map[uint32]string) []string { return nil }
