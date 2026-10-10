package darwin

import "testing"

func TestProcessName(t *testing.T) {
	cases := []struct {
		name, path, comm, want string
	}{
		{"full app path", "/Applications/Microsoft Teams.app/Contents/MacOS/MSTeams", "MSTeams", "MSTeams"},
		{"name longer than p_comm", "/Applications/Microsoft Teams.app/Contents/Helpers/Microsoft Teams WebView Helper", "Microsoft Teams ", "Microsoft Teams WebView Helper"},
		{"no path falls back to comm", "", "launchd", "launchd"},
		{"bare name", "zoom.us", "zoom.us", "zoom.us"},
		{"trailing slash ignored", "/usr/libexec/foo/", "foo", "foo"},
		{"root-only path falls back", "/", "kernel_task", "kernel_task"},
		{"both empty", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := processName(c.path, c.comm); got != c.want {
				t.Errorf("processName(%q, %q) = %q, want %q", c.path, c.comm, got, c.want)
			}
		})
	}
}

func TestCString(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte("Slack\x00\x00\x00"), "Slack"},
		{[]byte("no-terminator"), "no-terminator"},
		{[]byte("\x00garbage"), ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := cString(c.in); got != c.want {
			t.Errorf("cString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
