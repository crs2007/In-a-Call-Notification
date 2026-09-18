package windows

import "testing"

func TestUnmangleNonPackagedKey(t *testing.T) {
	cases := map[string]string{
		`C#Program Files#Teams#Teams.exe`: `C\Program Files\Teams\Teams.exe`,
		`plainname`:                       `plainname`,
		``:                                ``,
	}
	for in, want := range cases {
		if got := unmangleNonPackagedKey(in); got != want {
			t.Errorf("unmangleNonPackagedKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConsentEntryIsLive(t *testing.T) {
	cases := []struct {
		stop uint64
		want bool
	}{
		{stop: 0, want: true},
		{stop: 1, want: false},
		{stop: 132912345678900000, want: false},
	}
	for _, c := range cases {
		if got := consentEntryIsLive(c.stop); got != c.want {
			t.Errorf("consentEntryIsLive(%d) = %v, want %v", c.stop, got, c.want)
		}
	}
}

// TestFilterLiveConsentEntries_NonPackagedDroppedWhenProcessNotRunning is the
// fixture TODO.md 7.4 asks for: a Teams ConsentStore mic entry with
// Stop=0 (live per consentEntryIsLive) but ms-teams.exe not present in the
// current process list must not be reported as "in use" — it is a stale
// entry left behind by an app that already exited.
func TestFilterLiveConsentEntries_NonPackagedDroppedWhenProcessNotRunning(t *testing.T) {
	entries := []consentEntry{
		{name: `C:\Program Files\Teams\ms-teams.exe`, nonPackaged: true, live: true},
	}
	running := runningExeNameSet(map[uint32]string{}) // ms-teams.exe not running

	got := filterLiveConsentEntries(entries, running)
	if len(got) != 0 {
		t.Fatalf("filterLiveConsentEntries() = %v, want empty (process not running)", got)
	}
}

// TestFilterLiveConsentEntries_NonPackagedKeptWhenProcessRunning is the other
// half of the same fixture: the identical live entry is kept once
// ms-teams.exe shows up in the process list.
func TestFilterLiveConsentEntries_NonPackagedKeptWhenProcessRunning(t *testing.T) {
	entries := []consentEntry{
		{name: `C:\Program Files\Teams\ms-teams.exe`, nonPackaged: true, live: true},
	}
	running := runningExeNameSet(map[uint32]string{1234: "ms-teams.exe"})

	got := filterLiveConsentEntries(entries, running)
	want := []string{`C:\Program Files\Teams\ms-teams.exe`}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("filterLiveConsentEntries() = %v, want %v", got, want)
	}
}

func TestFilterLiveConsentEntries(t *testing.T) {
	running := runningExeNameSet(map[uint32]string{1: "ms-teams.exe", 2: "SLACK.EXE"})

	cases := []struct {
		name    string
		entries []consentEntry
		want    []string
	}{
		{
			name:    "not live is always dropped",
			entries: []consentEntry{{name: `C\zoom.exe`, nonPackaged: true, live: false}},
			want:    nil,
		},
		{
			name:    "case-insensitive exe match",
			entries: []consentEntry{{name: `C:\Program Files\Slack\slack.exe`, nonPackaged: true, live: true}},
			want:    []string{`C:\Program Files\Slack\slack.exe`},
		},
		{
			name:    "packaged entries pass through unfiltered, live or not gated by process list",
			entries: []consentEntry{{name: "MSTeams_8wekyb3d8bbwe", nonPackaged: false, live: true}},
			want:    []string{"MSTeams_8wekyb3d8bbwe"},
		},
		{
			name: "mixed: only the running NonPackaged entry and the packaged entry survive",
			entries: []consentEntry{
				{name: `C:\Program Files\Teams\ms-teams.exe`, nonPackaged: true, live: true},
				{name: `C:\Program Files\Zoom\zoom.exe`, nonPackaged: true, live: true},
				{name: "MSTeams_8wekyb3d8bbwe", nonPackaged: false, live: true},
			},
			want: []string{`C:\Program Files\Teams\ms-teams.exe`, "MSTeams_8wekyb3d8bbwe"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filterLiveConsentEntries(c.entries, running)
			if len(got) != len(c.want) {
				t.Fatalf("filterLiveConsentEntries() = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("filterLiveConsentEntries() = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestBaseExeNameLower(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\Teams\Teams.exe`: "teams.exe",
		`ms-teams.exe`:                     "ms-teams.exe",
		`C:\SLACK.EXE`:                     "slack.exe",
		``:                                 "",
	}
	for in, want := range cases {
		if got := baseExeNameLower(in); got != want {
			t.Errorf("baseExeNameLower(%q) = %q, want %q", in, got, want)
		}
	}
}
