package darwin

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenderPlistRoundTrips(t *testing.T) {
	tests := []struct {
		name, exe, cfg string
		wantArgs       []string
	}{
		{"plain", "/Applications/callmqtt", "", []string{"/Applications/callmqtt"}},
		{"spaces", "/Users/a b/My Apps/callmqtt", "", []string{"/Users/a b/My Apps/callmqtt"}},
		{"xml metacharacters", `/Users/x/R&D <"new">/callmqtt`, "", []string{`/Users/x/R&D <"new">/callmqtt`}},
		{"config", "/opt/cm/callmqtt", "/Users/s/work cfg/c.yaml", []string{"/opt/cm/callmqtt", "--config", "/Users/s/work cfg/c.yaml"}},
		{"unicode", "/Users/שלום/callmqtt", "", []string{"/Users/שלום/callmqtt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := renderPlist(tt.exe, tt.cfg)
			if err != nil {
				t.Fatalf("renderPlist: %v", err)
			}
			var doc plistDoc
			if err := xml.Unmarshal(body, &doc); err != nil {
				t.Fatalf("rendered plist is not XML: %v\n%s", err, body)
			}
			got := map[string]string{}
			var args []string
			for i := 0; i+1 < len(doc.Dict.Items); i += 2 {
				k, v := doc.Dict.Items[i].Text, doc.Dict.Items[i+1]
				if k == "ProgramArguments" {
					args = v.Strings
				}
				got[k] = v.XMLName.Local + ":" + v.Text
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("ProgramArguments = %q, want %q", args, tt.wantArgs)
			}
			want := map[string]string{
				"Label":       "string:" + LaunchAgentLabel,
				"RunAtLoad":   "true:",
				"KeepAlive":   "false:",
				"ProcessType": "string:Interactive",
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
			if exe, ok := parsePlistProgram(body); !ok || exe != tt.exe {
				t.Errorf("parsePlistProgram = %q,%v; want %q,true", exe, ok, tt.exe)
			}
		})
	}
}

func TestRenderPlistRejectsUnrepresentablePaths(t *testing.T) {
	for _, exe := range []string{"/tmp/a\x01b", "/tmp/\xff"} {
		if _, err := renderPlist(exe, ""); err == nil {
			t.Errorf("renderPlist(%q) succeeded; the path would be silently altered", exe)
		}
	}
	if _, err := renderPlist("/ok", "/cfg\x00"); err == nil {
		t.Error("a bad --config path must be refused too")
	}
}

func TestParsePlistProgram(t *testing.T) {
	const head = `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>`
	tests := []struct {
		name, body, want string
		ok               bool
	}{
		{"program arguments", head + `<key>Label</key><string>x</string><key>ProgramArguments</key><array><string>/a/cm</string><string>--debug</string></array></dict></plist>`, "/a/cm", true},
		{"program wins", head + `<key>ProgramArguments</key><array><string>/a/cm</string></array><key>Program</key><string>/b/cm</string></dict></plist>`, "/b/cm", true},
		{"escaped", head + `<key>ProgramArguments</key><array><string>/R&amp;D/cm</string></array></dict></plist>`, "/R&D/cm", true},
		{"empty array", head + `<key>ProgramArguments</key><array></array></dict></plist>`, "", false},
		{"no program", head + `<key>Label</key><string>x</string></dict></plist>`, "", false},
		{"not xml (binary plist)", "bplist00\x00\x01", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePlistProgram([]byte(tt.body))
			if got != tt.want || ok != tt.ok {
				t.Errorf("parsePlistProgram = %q,%v; want %q,%v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSameExePath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "callmqtt")
	if err := os.WriteFile(real, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	haveLink := os.Symlink(real, link) == nil

	tests := []struct {
		name           string
		stored, actual string
		want, needLink bool
	}{
		{"identical", real, real, true, false},
		{"unclean stored path", filepath.Join(dir, ".", "callmqtt"), real, true, false},
		{"moved install", filepath.Join(dir, "old", "callmqtt"), real, false, false},
		{"symlink resolves", link, real, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.needLink && !haveLink {
				t.Skip("symlinks unavailable")
			}
			if got := sameExePath(tt.stored, tt.actual); got != tt.want {
				t.Errorf("sameExePath(%q, %q) = %v, want %v", tt.stored, tt.actual, got, tt.want)
			}
		})
	}
}

// fakeLaunchctl records every launchctl call and fails the ones listed in
// codes with that exit status.
type fakeLaunchctl struct {
	calls []string
	codes map[string]int // verb ("bootout", "bootstrap") -> exit status
}

func (f *fakeLaunchctl) run(name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, strings.Join(append([]string{filepath.Base(name)}, args...), " "))
	if code, ok := f.codes[args[0]]; ok {
		return nil, nil, &exitError{name: name, code: code}
	}
	return nil, nil, nil
}

func testAgent(t *testing.T, f *fakeLaunchctl, exe string) launchAgent {
	return launchAgent{
		plistPath: filepath.Join(t.TempDir(), "LaunchAgents", LaunchAgentLabel+".plist"),
		domain:    "gui/501",
		exe:       func() (string, error) { return exe, nil },
		run:       f.run,
	}
}

func TestLaunchAgentEnable(t *testing.T) {
	const exe = "/Applications/cm/callmqtt"
	bootout := "launchctl bootout gui/501/" + LaunchAgentLabel
	tests := []struct {
		name      string
		load      bool
		codes     map[string]int
		wantCalls []string // plist path is appended to bootstrap below
		wantErr   bool
	}{
		{"load replaces a loaded copy", true, nil, []string{bootout, "bootstrap"}, false},
		{"load when nothing was loaded", true, map[string]int{"bootout": 3}, []string{bootout, "bootstrap"}, false},
		{"load on a release reporting 113", true, map[string]int{"bootout": 113}, []string{bootout, "bootstrap"}, false},
		{"bootout failing otherwise stops", true, map[string]int{"bootout": 1}, []string{bootout}, true},
		{"bootstrap failure is reported", true, map[string]int{"bootstrap": 5}, []string{bootout, "bootstrap"}, true},
		{"write only, for a running agent", false, nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeLaunchctl{codes: tt.codes}
			a := testAgent(t, f, exe)
			err := a.enable("/cfg/c.yaml", tt.load)
			if (err != nil) != tt.wantErr {
				t.Fatalf("enable: err = %v, wantErr %v", err, tt.wantErr)
			}
			want := append([]string(nil), tt.wantCalls...)
			for i, c := range want {
				if c == "bootstrap" {
					want[i] = "launchctl bootstrap gui/501 " + a.plistPath
				}
			}
			if !reflect.DeepEqual(f.calls, want) {
				t.Errorf("launchctl calls = %q, want %q", f.calls, want)
			}

			info, err := os.Stat(a.plistPath)
			if err != nil {
				t.Fatalf("plist not written: %v", err)
			}
			if perm := info.Mode().Perm(); filepath.Separator == '/' && perm != 0o644 {
				t.Errorf("plist mode = %v, want 0644", perm)
			}
			enabled, stale, target, err := a.status()
			if err != nil || !enabled || stale || target != exe {
				t.Errorf("status after enable = %v,%v,%q,%v; want true,false,%q,nil", enabled, stale, target, err, exe)
			}
		})
	}
}

func TestLaunchAgentEnableRefusesTranslocatedApp(t *testing.T) {
	f := &fakeLaunchctl{}
	a := testAgent(t, f, "/private/var/folders/x/T/AppTranslocation/1234/d/In a Call.app/Contents/MacOS/callmqtt")
	err := a.enable("", true)
	if err == nil || !strings.Contains(err.Error(), "/Applications") {
		t.Fatalf("enable = %v; want a refusal telling the user to move the app", err)
	}
	if _, statErr := os.Stat(a.plistPath); !errors.Is(statErr, os.ErrNotExist) || len(f.calls) != 0 {
		t.Errorf("a refused enable wrote a plist (%v) or ran launchctl %q", statErr, f.calls)
	}
}

func TestLaunchAgentEnableFixesLooseMode(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("unix permissions")
	}
	a := testAgent(t, &fakeLaunchctl{}, "/cm")
	if err := os.MkdirAll(filepath.Dir(a.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.plistPath, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(a.plistPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := a.enable("", false); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(a.plistPath); info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v; launchd ignores a group/world-writable plist", info.Mode().Perm())
	}
}

func TestLaunchAgentDisable(t *testing.T) {
	bootout := "launchctl bootout gui/501/" + LaunchAgentLabel
	tests := []struct {
		name      string
		installed bool
		unload    bool
		codes     map[string]int
		wantCalls []string
		wantErr   bool
	}{
		{"installed and loaded", true, true, nil, []string{bootout}, false},
		{"nothing installed is fine", false, true, map[string]int{"bootout": 3}, []string{bootout}, false},
		{"file only, for a running agent", true, false, nil, nil, false},
		{"unexpected bootout failure keeps the file", true, true, map[string]int{"bootout": 1}, []string{bootout}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeLaunchctl{codes: tt.codes}
			a := testAgent(t, f, "/cm")
			if tt.installed {
				if err := a.enable("", false); err != nil {
					t.Fatal(err)
				}
			}
			err := a.disable(tt.unload)
			if (err != nil) != tt.wantErr {
				t.Fatalf("disable: err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(f.calls, tt.wantCalls) {
				t.Errorf("launchctl calls = %q, want %q", f.calls, tt.wantCalls)
			}
			_, statErr := os.Stat(a.plistPath)
			if gone := errors.Is(statErr, os.ErrNotExist); gone == tt.wantErr {
				t.Errorf("plist removed = %v, want %v", gone, !tt.wantErr)
			}
		})
	}
}

func TestLaunchAgentStatus(t *testing.T) {
	tests := []struct {
		name               string
		plist              string // "" = no file
		wantEnabled, stale bool
		wantTarget         string
	}{
		{"not installed", "", false, false, ""},
		{"current exe", "render:/Applications/cm/callmqtt", true, false, "/Applications/cm/callmqtt"},
		{"moved install", "render:/Users/s/Downloads/callmqtt", true, true, "/Users/s/Downloads/callmqtt"},
		{"unreadable plist", "bplist00", true, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := testAgent(t, &fakeLaunchctl{}, "/Applications/cm/callmqtt")
			if tt.plist != "" {
				body := []byte(tt.plist)
				if exe, ok := strings.CutPrefix(tt.plist, "render:"); ok {
					var err error
					if body, err = renderPlist(exe, ""); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.MkdirAll(filepath.Dir(a.plistPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(a.plistPath, body, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			enabled, stale, target, err := a.status()
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if enabled != tt.wantEnabled || stale != tt.stale || target != tt.wantTarget {
				t.Errorf("status = %v,%v,%q; want %v,%v,%q", enabled, stale, target, tt.wantEnabled, tt.stale, tt.wantTarget)
			}
		})
	}
}
