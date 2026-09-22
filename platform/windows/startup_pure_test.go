package windows

import "testing"

func TestFormatRunValue(t *testing.T) {
	tests := []struct {
		name       string
		exe        string
		configPath string
		want       string
	}{
		{
			name: "default config, no flag appended",
			exe:  `C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe`,
			want: `"C:\Users\Sharon\AppData\Local\callmqtt\callmqtt.exe"`,
		},
		{
			name:       "explicit config path appended and quoted",
			exe:        `C:\Program Files\callmqtt\callmqtt.exe`,
			configPath: `D:\work\callmqtt.yaml`,
			want:       `"C:\Program Files\callmqtt\callmqtt.exe" --config "D:\work\callmqtt.yaml"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatRunValue(tt.exe, tt.configPath); got != tt.want {
				t.Errorf("formatRunValue(%q, %q) = %q, want %q", tt.exe, tt.configPath, got, tt.want)
			}
		})
	}
}

func TestParseRunValueExe(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantExe string
		wantOK  bool
	}{
		{
			name:    "exe only",
			value:   `"C:\callmqtt\callmqtt.exe"`,
			wantExe: `C:\callmqtt\callmqtt.exe`,
			wantOK:  true,
		},
		{
			name:    "exe with config flag",
			value:   `"C:\callmqtt\callmqtt.exe" --config "D:\work\callmqtt.yaml"`,
			wantExe: `C:\callmqtt\callmqtt.exe`,
			wantOK:  true,
		},
		{
			name:   "legacy Go %q-escaped value is not confidently parseable as-is",
			value:  `"C:\\callmqtt\\callmqtt.exe"`,
			wantOK: true,
			// parseRunValueExe only extracts the quoted token; it is up to
			// the caller (runValueMatchesExe) to decide a doubled-backslash
			// value does not match the real exe path.
			wantExe: `C:\\callmqtt\\callmqtt.exe`,
		},
		{
			name:   "unquoted value",
			value:  `C:\callmqtt\callmqtt.exe`,
			wantOK: false,
		},
		{
			name:   "empty value",
			value:  "",
			wantOK: false,
		},
		{
			name:   "unterminated quote",
			value:  `"C:\callmqtt\callmqtt.exe`,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExe, gotOK := parseRunValueExe(tt.value)
			if gotOK != tt.wantOK || (gotOK && gotExe != tt.wantExe) {
				t.Errorf("parseRunValueExe(%q) = (%q, %v), want (%q, %v)", tt.value, gotExe, gotOK, tt.wantExe, tt.wantOK)
			}
		})
	}
}

func TestRunValueMatchesExe(t *testing.T) {
	tests := []struct {
		name        string
		storedValue string
		exe         string
		want        bool
	}{
		{
			name:        "exact match",
			storedValue: `"C:\callmqtt\callmqtt.exe"`,
			exe:         `C:\callmqtt\callmqtt.exe`,
			want:        true,
		},
		{
			name:        "case-insensitive match",
			storedValue: `"C:\CallMQTT\CallMQTT.exe"`,
			exe:         `C:\callmqtt\callmqtt.exe`,
			want:        true,
		},
		{
			name:        "match ignores appended --config",
			storedValue: `"C:\callmqtt\callmqtt.exe" --config "D:\work\callmqtt.yaml"`,
			exe:         `C:\callmqtt\callmqtt.exe`,
			want:        true,
		},
		{
			name:        "stale path after move",
			storedValue: `"C:\old\callmqtt.exe"`,
			exe:         `C:\new\callmqtt.exe`,
			want:        false,
		},
		{
			name:        "unparseable stored value",
			storedValue: `not a quoted value`,
			exe:         `C:\callmqtt\callmqtt.exe`,
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runValueMatchesExe(tt.storedValue, tt.exe); got != tt.want {
				t.Errorf("runValueMatchesExe(%q, %q) = %v, want %v", tt.storedValue, tt.exe, got, tt.want)
			}
		})
	}
}
