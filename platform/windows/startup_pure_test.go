package windows

import "testing"

func TestRunCommand(t *testing.T) {
	tests := []struct {
		name, exe, cfg, want string
	}{
		{"plain", `C:\callmqtt\callmqtt.exe`, "", `"C:\callmqtt\callmqtt.exe"`},
		{"spaces", `C:\Program Files\cm\callmqtt.exe`, "", `"C:\Program Files\cm\callmqtt.exe"`},
		{"unc", `\\srv\share\callmqtt.exe`, "", `"\\srv\share\callmqtt.exe"`},
		{"config", `C:\cm\callmqtt.exe`, `D:\my work\c.yaml`, `"C:\cm\callmqtt.exe" --config "D:\my work\c.yaml"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runCommand(tt.exe, tt.cfg); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseRunCommand(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{`"C:\cm\callmqtt.exe"`, `C:\cm\callmqtt.exe`, true},
		{`"C:\Program Files\cm\callmqtt.exe" --config "D:\c.yaml"`, `C:\Program Files\cm\callmqtt.exe`, true},
		{`"C:\\Users\\S\\callmqtt.exe"`, `C:\\Users\\S\\callmqtt.exe`, true},
		{`C:\cm\callmqtt.exe --debug`, `C:\cm\callmqtt.exe`, true},
		{`C:\cm\callmqtt.exe`, `C:\cm\callmqtt.exe`, true},
		{``, ``, false},
		{`"unterminated`, ``, false},
		{`""`, ``, false},
	}
	for _, tt := range tests {
		got, ok := parseRunCommand(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseRunCommand(%q) = %q,%v; want %q,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSameExePath(t *testing.T) {
	if !sameExePath(`C:\Users\S\cm.exe`, `c:\users\s\CM.exe`) {
		t.Error("case difference should match")
	}
	if !sameExePath(`C:\\Users\\S\\cm.exe`, `C:\Users\S\cm.exe`) {
		t.Error("legacy doubled backslashes should match")
	}
	if sameExePath(`C:\old\cm.exe`, `C:\new\cm.exe`) {
		t.Error("different dirs must not match")
	}
}
