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

// Times below are FILETIMEs (100 ns ticks); only their ordering matters.
// A "second" is 1e7 ticks.
const ft = uint64(10_000_000)

// staticProcs adapts a fixed process list to filterLiveConsentEntries'
// lazy resolver shape and counts how often it was asked.
func staticProcs(procs ...runningProcess) (func() []runningProcess, *int) {
	calls := 0
	return func() []runningProcess {
		calls++
		return procs
	}, &calls
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
	running, _ := staticProcs(runningProcess{exe: "explorer.exe", start: 1 * ft}) // ms-teams.exe not running

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
	running, _ := staticProcs(runningProcess{exe: "ms-teams.exe"})

	got := filterLiveConsentEntries(entries, running)
	want := []string{`C:\Program Files\Teams\ms-teams.exe`}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("filterLiveConsentEntries() = %v, want %v", got, want)
	}
}

// TestFilterLiveConsentEntries_PackagedStaleAfterRelaunch is the regression
// test for issue #8. New Teams (packaged MSTeams_8wekyb3d8bbwe) was in a
// call, crashed, and was relaunched: the ConsentStore entry is still "live"
// (Windows never wrote a Stop time) and ms-teams.exe *is* running again, but
// every running instance was created after the entry's LastUsedTimeStart,
// so none of them can own that capture. The entry must be dropped. The same
// entry with a process that predates it - the app started, then the call
// began - is the genuine in-call shape and must be kept.
func TestFilterLiveConsentEntries_PackagedStaleAfterRelaunch(t *testing.T) {
	const family = "MSTeams_8wekyb3d8bbwe"
	callStart := 100_000 * ft
	entry := consentEntry{name: family, live: true, start: callStart}

	cases := []struct {
		name  string
		procs []runningProcess
		want  bool
	}{
		{
			name:  "relaunched after the crash: every instance is younger than the capture",
			procs: []runningProcess{{exe: "ms-teams.exe", family: family, start: callStart + 600*ft}},
			want:  false,
		},
		{
			name: "several instances, all relaunched",
			procs: []runningProcess{
				{exe: "ms-teams.exe", family: family, start: callStart + 600*ft},
				{exe: "ms-teams.exe", family: family, start: callStart + 601*ft},
			},
			want: false,
		},
		{
			name:  "not running at all",
			procs: []runningProcess{{exe: "explorer.exe", start: 1 * ft}},
			want:  false,
		},
		{
			name:  "genuine call: the app predates the capture",
			procs: []runningProcess{{exe: "ms-teams.exe", family: family, start: callStart - 3600*ft}},
			want:  true,
		},
		{
			name: "genuine call: one old instance among relaunched helpers",
			procs: []runningProcess{
				{exe: "ms-teams.exe", family: family, start: callStart + 5*ft},
				{exe: "ms-teams.exe", family: family, start: callStart - 3600*ft},
			},
			want: true,
		},
		{
			name:  "same exe name but a different package family is not the owner",
			procs: []runningProcess{{exe: "ms-teams.exe", family: "Other_1234abcd", start: callStart - 3600*ft}},
			want:  false,
		},
		{
			name:  "unpackaged process of the same exe name is not the owner of a packaged entry",
			procs: []runningProcess{{exe: "ms-teams.exe", start: callStart - 3600*ft}},
			want:  false,
		},
		{
			name:  "family names compare case-insensitively",
			procs: []runningProcess{{exe: "ms-teams.exe", family: "msteams_8WEKYB3D8BBWE", start: callStart - 1*ft}},
			want:  true,
		},
		{
			name:  "within consentStartSlack of the capture still counts as owner",
			procs: []runningProcess{{exe: "ms-teams.exe", family: family, start: callStart + consentStartSlack}},
			want:  true,
		},
		{
			name:  "one tick past consentStartSlack does not",
			procs: []runningProcess{{exe: "ms-teams.exe", family: family, start: callStart + consentStartSlack + 1}},
			want:  false,
		},
		{
			name:  "unknown process start time disables the time check",
			procs: []runningProcess{{exe: "ms-teams.exe", family: family, start: 0}},
			want:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			running, _ := staticProcs(c.procs...)
			got := filterLiveConsentEntries([]consentEntry{entry}, running)
			if kept := len(got) == 1; kept != c.want {
				t.Fatalf("filterLiveConsentEntries() = %v, want kept=%v", got, c.want)
			}
		})
	}
}

// TestFilterLiveConsentEntries_NonPackagedStaleAfterRelaunch: the same
// crash-and-relaunch shape for a classic (NonPackaged) app such as Zoom,
// which the exe-name check alone could never catch.
func TestFilterLiveConsentEntries_NonPackagedStaleAfterRelaunch(t *testing.T) {
	callStart := 100_000 * ft
	entry := consentEntry{name: `C:\Users\me\AppData\Roaming\Zoom\bin\Zoom.exe`, nonPackaged: true, live: true, start: callStart}

	relaunched, _ := staticProcs(runningProcess{exe: "zoom.exe", start: callStart + 60*ft})
	if got := filterLiveConsentEntries([]consentEntry{entry}, relaunched); len(got) != 0 {
		t.Fatalf("relaunched: filterLiveConsentEntries() = %v, want empty", got)
	}

	inCall, _ := staticProcs(runningProcess{exe: "zoom.exe", start: callStart - 60*ft})
	if got := filterLiveConsentEntries([]consentEntry{entry}, inCall); len(got) != 1 {
		t.Fatalf("in call: filterLiveConsentEntries() = %v, want the entry", got)
	}
}

// TestFilterLiveConsentEntries_UnknownEntryStartSkipsTimeCheck: an entry
// whose LastUsedTimeStart could not be read still gets the process-name
// check, but never the time check - "unknown" is not evidence of staleness.
func TestFilterLiveConsentEntries_UnknownEntryStartSkipsTimeCheck(t *testing.T) {
	entry := consentEntry{name: "MSTeams_8wekyb3d8bbwe", live: true, start: 0}
	running, _ := staticProcs(runningProcess{exe: "ms-teams.exe", family: "MSTeams_8wekyb3d8bbwe", start: 5_000 * ft})
	if got := filterLiveConsentEntries([]consentEntry{entry}, running); len(got) != 1 {
		t.Fatalf("filterLiveConsentEntries() = %v, want the entry", got)
	}
}

// TestFilterLiveConsentEntries_ResolvesProcessesLazily: the process resolver
// is expensive (one OpenProcess per PID), so it must not run on an idle poll
// with no live entries, and must run at most once when there are several.
func TestFilterLiveConsentEntries_ResolvesProcessesLazily(t *testing.T) {
	running, calls := staticProcs(runningProcess{exe: "ms-teams.exe"})

	idle := []consentEntry{
		{name: `C:\zoom.exe`, nonPackaged: true, live: false},
		{name: "MSTeams_8wekyb3d8bbwe", live: false},
	}
	filterLiveConsentEntries(idle, running)
	if *calls != 0 {
		t.Fatalf("idle poll resolved processes %d times, want 0", *calls)
	}

	busy := []consentEntry{
		{name: `C:\ms-teams.exe`, nonPackaged: true, live: true},
		{name: `C:\zoom.exe`, nonPackaged: true, live: true},
		{name: "MSTeams_8wekyb3d8bbwe", live: true},
	}
	filterLiveConsentEntries(busy, running)
	if *calls != 1 {
		t.Fatalf("busy poll resolved processes %d times, want 1", *calls)
	}
}

// TestFilterLiveConsentEntries_NoProcessEvidenceKeepsLiveEntries: if the
// resolver is absent or returns nothing (every OpenProcess failed), the
// filter has no evidence to drop anything on, and keeps the "no evidence is
// not failure" posture of the rest of the package.
func TestFilterLiveConsentEntries_NoProcessEvidenceKeepsLiveEntries(t *testing.T) {
	entries := []consentEntry{{name: "MSTeams_8wekyb3d8bbwe", live: true, start: 1 * ft}}
	if got := filterLiveConsentEntries(entries, nil); len(got) != 1 {
		t.Fatalf("nil resolver: filterLiveConsentEntries() = %v, want the entry", got)
	}
	empty, _ := staticProcs()
	if got := filterLiveConsentEntries(entries, empty); len(got) != 1 {
		t.Fatalf("empty resolver: filterLiveConsentEntries() = %v, want the entry", got)
	}
}

func TestFilterLiveConsentEntries(t *testing.T) {
	running, _ := staticProcs(
		runningProcess{exe: "ms-teams.exe", family: "MSTeams_8wekyb3d8bbwe"},
		runningProcess{exe: "slack.exe"},
	)

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
			entries: []consentEntry{{name: `C:\Program Files\Slack\SLACK.EXE`, nonPackaged: true, live: true}},
			want:    []string{`C:\Program Files\Slack\SLACK.EXE`},
		},
		{
			name:    "packaged entry kept when a process of that family is running",
			entries: []consentEntry{{name: "MSTeams_8wekyb3d8bbwe", nonPackaged: false, live: true}},
			want:    []string{"MSTeams_8wekyb3d8bbwe"},
		},
		{
			name:    "packaged entry dropped when no process of that family is running",
			entries: []consentEntry{{name: "Microsoft.SkypeApp_kzf8qxf38zg5c", nonPackaged: false, live: true}},
			want:    nil,
		},
		{
			name: "mixed: only entries with a running owner survive",
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
