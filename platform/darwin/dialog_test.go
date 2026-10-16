//go:build darwin

package darwin

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShowAlertPassesTextAsArguments(t *testing.T) {
	tests := []struct {
		name, style, title, message string
	}{
		{"error", "critical", "In a Call Notification", "no config yet"},
		{"info", "informational", "In a Call Notification", "Wrote /Users/x/config.yaml"},
		{"script injection stays data", "critical", `t" & (do shell script "id") & "`, `m"` + "\nend run\n"},
		{"leading dash is not a flag", "critical", "-x", "--config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			showAlert(func(name string, args ...string) ([]byte, []byte, error) {
				got = append([]string{name}, args...)
				return nil, nil, nil
			}, tt.style, tt.title, tt.message)

			n := len(got)
			if n < 4 || got[0] != osascript || got[n-3] != "--" || got[n-2] != tt.title || got[n-1] != tt.message {
				t.Fatalf("osascript args = %q; want title and message as the last two arguments after --", got)
			}
			for _, a := range got[1 : n-3] {
				if a == tt.title || a == tt.message {
					t.Errorf("user text %q appears in the script itself", a)
				}
			}
			if !reflect.DeepEqual(got[1:n-3], alertArgs(tt.style, "", "")[:6]) {
				t.Errorf("script = %q, want the fixed alert script", got[1:n-3])
			}
		})
	}
}

// TestAlertScriptCompiles checks the AppleScript is valid without showing
// anything: osacompile parses and compiles it but never runs it.
func TestAlertScriptCompiles(t *testing.T) {
	osacompile, err := exec.LookPath("osacompile")
	if err != nil {
		t.Skip("osacompile not available")
	}
	for _, style := range []string{"critical", "informational"} {
		args := alertArgs(style, "", "")[:6]
		out := filepath.Join(t.TempDir(), "alert.scpt")
		if msg, err := exec.Command(osacompile, append(args, "-o", out)...).CombinedOutput(); err != nil {
			t.Errorf("%s alert script does not compile: %v\n%s", style, err, msg)
		}
	}
}
