package windows

import "testing"

func TestBuildRunValue(t *testing.T) {
	cases := []struct {
		name                                     string
		exe, configPath, defaultConfigPath, want string
	}{
		{
			name:              "default config omits --config",
			exe:               `C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe`,
			configPath:        `C:\Users\Sharon\AppData\Roaming\callmqtt\config.yaml`,
			defaultConfigPath: `C:\Users\Sharon\AppData\Roaming\callmqtt\config.yaml`,
			want:              `"C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe"`,
		},
		{
			name:              "custom config appends --config",
			exe:               `C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe`,
			configPath:        `D:\work\callmqtt.yaml`,
			defaultConfigPath: `C:\Users\Sharon\AppData\Roaming\callmqtt\config.yaml`,
			want:              `"C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe" --config "D:\work\callmqtt.yaml"`,
		},
		{
			name:              "path with spaces",
			exe:               `C:\Program Files\callmqtt\callmqtt.exe`,
			configPath:        `C:\Users\A B\config.yaml`,
			defaultConfigPath: `C:\default\config.yaml`,
			want:              `"C:\Program Files\callmqtt\callmqtt.exe" --config "C:\Users\A B\config.yaml"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildRunValue(c.exe, c.configPath, c.defaultConfigPath)
			if got != c.want {
				t.Errorf("buildRunValue(%q, %q, %q) = %q, want %q", c.exe, c.configPath, c.defaultConfigPath, got, c.want)
			}
		})
	}
}

func TestRunValueExe(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantExe string
		wantOK  bool
	}{
		{name: "plain", value: `"C:\callmqtt.exe"`, wantExe: `C:\callmqtt.exe`, wantOK: true},
		{name: "with args", value: `"C:\callmqtt.exe" --config "D:\c.yaml"`, wantExe: `C:\callmqtt.exe`, wantOK: true},
		{name: "empty", value: "", wantOK: false},
		{name: "no leading quote", value: `C:\callmqtt.exe`, wantOK: false},
		{name: "no closing quote", value: `"C:\callmqtt.exe`, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotExe, gotOK := runValueExe(c.value)
			if gotOK != c.wantOK || (gotOK && gotExe != c.wantExe) {
				t.Errorf("runValueExe(%q) = (%q, %v), want (%q, %v)", c.value, gotExe, gotOK, c.wantExe, c.wantOK)
			}
		})
	}
}

func TestRunValueMatchesExe(t *testing.T) {
	cases := []struct {
		name  string
		value string
		exe   string
		want  bool
	}{
		{
			name:  "matches",
			value: `"C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe" --config "D:\c.yaml"`,
			exe:   `C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe`,
			want:  true,
		},
		{
			name:  "matches case-insensitively",
			value: `"c:\users\sharon\appdata\local\callmqtt\callmqtt.exe"`,
			exe:   `C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe`,
			want:  true,
		},
		{
			name:  "stale path",
			value: `"D:\old-location\callmqtt.exe"`,
			exe:   `C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe`,
			want:  false,
		},
		{
			name:  "malformed value",
			value: `not-quoted`,
			exe:   `C:\callmqtt.exe`,
			want:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runValueMatchesExe(c.value, c.exe); got != c.want {
				t.Errorf("runValueMatchesExe(%q, %q) = %v, want %v", c.value, c.exe, got, c.want)
			}
		})
	}
}
