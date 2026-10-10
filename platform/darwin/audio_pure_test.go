package darwin

import (
	"reflect"
	"testing"
)

// TestFourCCConstants pins every numeric selector to the four-character
// code Apple's headers spell it with, so a mistyped hex literal fails here
// rather than silently reading the wrong property on a Mac.
func TestFourCCConstants(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		code string
	}{
		{"scopeGlobal", scopeGlobal, "glob"},
		{"selProcessObjectList", selProcessObjectList, "prs#"},
		{"selProcessPID", selProcessPID, "ppid"},
		{"selProcessBundleID", selProcessBundleID, "pbid"},
		{"selProcessIsRunningInput", selProcessIsRunningInput, "piri"},
		{"selProcessIsRunningOutput", selProcessIsRunningOutput, "piro"},
		{"selDefaultInputDevice", selDefaultInputDevice, "dIn "},
		{"selDeviceIsRunningSomewhere", selDeviceIsRunningSomewhere, "gone"},
		{"selCMIODevices", selCMIODevices, "dev#"},
	}
	for _, c := range cases {
		if want := fourCC(c.code); c.got != want {
			t.Errorf("%s = %#x, want fourCC(%q) = %#x", c.name, c.got, c.code, want)
		}
	}
}

func TestFourCC(t *testing.T) {
	cases := map[string]uint32{
		"glob":  0x676c6f62,
		"dIn ":  0x64496e20,
		"":      0,
		"abc":   0,
		"abcde": 0,
	}
	for in, want := range cases {
		if got := fourCC(in); got != want {
			t.Errorf("fourCC(%q) = %#x, want %#x", in, got, want)
		}
	}
}

func TestMicrophoneUsers(t *testing.T) {
	procNames := map[uint32]string{
		100: "MSTeams",
		200: "sox",
	}
	cases := []struct {
		name  string
		procs []audioProcess
		want  []string
	}{
		{
			name:  "nothing capturing",
			procs: []audioProcess{{pid: 100, bundleID: "com.microsoft.teams2"}},
			want:  nil,
		},
		{
			// Phase 0 finding: Spotify playing music has output running but
			// not input, so it must not count as microphone use.
			name: "output-only process ignored",
			procs: []audioProcess{
				{pid: 300, bundleID: "com.spotify.client", inputRunning: false},
				{pid: 100, bundleID: "com.microsoft.teams2", inputRunning: true},
			},
			want: []string{"com.microsoft.teams2"},
		},
		{
			name: "sorted and de-duplicated across helper processes",
			procs: []audioProcess{
				{pid: 401, bundleID: "us.zoom.xos", inputRunning: true},
				{pid: 100, bundleID: "com.microsoft.teams2", inputRunning: true},
				{pid: 402, bundleID: "us.zoom.xos", inputRunning: true},
			},
			want: []string{"com.microsoft.teams2", "us.zoom.xos"},
		},
		{
			name:  "missing bundle falls back to process name",
			procs: []audioProcess{{pid: 200, inputRunning: true}},
			want:  []string{"sox"},
		},
		{
			name: "no bundle and unknown pid reports unknown once",
			procs: []audioProcess{
				{pid: 999, inputRunning: true},
				{pid: 0, inputRunning: true},
			},
			want: []string{unknownApp},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := microphoneUsers(c.procs, procNames); !reflect.DeepEqual(got, c.want) {
				t.Errorf("microphoneUsers() = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestDeviceLevelUsers(t *testing.T) {
	if got := deviceLevelUsers(false); got != nil {
		t.Errorf("deviceLevelUsers(false) = %#v, want nil", got)
	}
	if got := deviceLevelUsers(true); !reflect.DeepEqual(got, []string{"(unknown)"}) {
		t.Errorf("deviceLevelUsers(true) = %#v, want [(unknown)]", got)
	}
}
