package darwin

// ScreenRecordingSettingsURL opens System Settings at Privacy & Security >
// Screen & System Audio Recording, where the user grants the permission
// VisibleWindows needs to read window titles. Pass it to `open`.
//
// It lives in an untagged file so cross-platform UI code (the tray, the
// first-launch explainer) can reference it without a build tag.
const ScreenRecordingSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
