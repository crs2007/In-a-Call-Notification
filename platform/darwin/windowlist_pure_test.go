package darwin

import (
	"reflect"
	"testing"
)

func TestFilterWindows(t *testing.T) {
	cases := []struct {
		name string
		raw  []rawWindow
		want []WindowInfo
	}{
		{
			name: "normal titled window kept",
			raw:  []rawWindow{{pid: 42, hasPID: true, layer: 0, hasLayer: true, name: "Meeting | Microsoft Teams"}},
			want: []WindowInfo{{PID: 42, Title: "Meeting | Microsoft Teams"}},
		},
		{
			// Screen Recording not granted: macOS blanks the title, but
			// the owning PID is still process evidence.
			name: "untitled window kept for its owner",
			raw:  []rawWindow{{pid: 42, hasPID: true, layer: 0, hasLayer: true, name: ""}},
			want: []WindowInfo{{PID: 42, Title: ""}},
		},
		{
			name: "menu bar and status items dropped",
			raw: []rawWindow{
				{pid: 7, hasPID: true, layer: 24, hasLayer: true, name: "Menubar"},
				{pid: 8, hasPID: true, layer: 25, hasLayer: true, name: "Item-0"},
			},
			want: []WindowInfo{},
		},
		{
			name: "missing layer dropped",
			raw:  []rawWindow{{pid: 42, hasPID: true, name: "Zoom Meeting"}},
			want: []WindowInfo{},
		},
		{
			name: "missing or invalid owner dropped",
			raw: []rawWindow{
				{layer: 0, hasLayer: true, name: "orphan"},
				{pid: -1, hasPID: true, layer: 0, hasLayer: true, name: "negative"},
				{pid: 0, hasPID: true, layer: 0, hasLayer: true, name: "zero"},
			},
			want: []WindowInfo{},
		},
		{
			name: "order preserved across a mixed list",
			raw: []rawWindow{
				{pid: 1, hasPID: true, layer: 0, hasLayer: true, name: "A"},
				{pid: 2, hasPID: true, layer: 3, hasLayer: true, name: "floating"},
				{pid: 3, hasPID: true, layer: 0, hasLayer: true, name: "B"},
			},
			want: []WindowInfo{{PID: 1, Title: "A"}, {PID: 3, Title: "B"}},
		},
		{name: "empty input", raw: nil, want: []WindowInfo{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := filterWindows(c.raw); !reflect.DeepEqual(got, c.want) {
				t.Errorf("filterWindows() = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestScreenRecordingSettingsURL(t *testing.T) {
	const want = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	if ScreenRecordingSettingsURL != want {
		t.Fatalf("ScreenRecordingSettingsURL = %q, want %q", ScreenRecordingSettingsURL, want)
	}
}
