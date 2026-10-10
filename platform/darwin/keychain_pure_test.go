package darwin

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestKeychainPassword(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		err      error
		want     string
		wantErr  error // checked with errors.Is
		wantFail bool
	}{
		{"printable", "password: \"s3cret\"\n", nil, "s3cret", nil, false},
		{"non-ASCII", "password: 0x70C3A4  \"p\\303\\244\"\n", nil, "pä", nil, false},
		{"not found", "", &exitError{name: securityTool, code: 44}, "", ErrKeychainItemNotFound, true},
		{"other failure", "", &exitError{name: securityTool, code: 51, output: "User interaction is not allowed."}, "", nil, true},
		{"no password line", "keychain: \"/x\"\n", nil, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotArgs []string
			run := func(name string, args ...string) ([]byte, []byte, error) {
				gotArgs = append([]string{name}, args...)
				return []byte("keychain: \"login.keychain-db\"\n"), []byte(tt.stderr), tt.err
			}
			got, err := keychainPassword(run, "callmqtt", "bob")
			if (err != nil) != tt.wantFail {
				t.Fatalf("err = %v, wantFail %v", err, tt.wantFail)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && errors.Is(err, ErrKeychainItemNotFound) {
				t.Errorf("err = %v must not claim the item is missing", err)
			}
			if got != tt.want {
				t.Errorf("password = %q, want %q", got, tt.want)
			}
			wantArgs := []string{securityTool, "find-generic-password", "-s", "callmqtt", "-a", "bob", "-g"}
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Errorf("ran %q, want %q", gotArgs, wantArgs)
			}
		})
	}
}

// TestParseSecurityPassword uses output captured from macOS 26.6's security
// for throwaway items holding each password.
func TestParseSecurityPassword(t *testing.T) {
	tests := []struct {
		name, stderr, want string
		wantErr            bool
	}{
		{"plain", `password: "hunter2"` + "\n", "hunter2", false},
		{"hex-looking text stays text", `password: "0a1b"` + "\n", "0a1b", false},
		{"quote and backslash use hex", `password: 0x6122625C63  "a\"b\134c"` + "\n", `a"b\c`, false},
		{"non-ASCII uses hex", `password: 0x70C3A422  "p\303\244""` + "\n", `pä"`, false},
		{"spaces kept", `password: " a b "` + "\n", " a b ", false},
		{"empty", "password: \n", "", false},
		{"other lines ignored", "noise\npassword: \"x\"\n", "x", false},
		{"bad hex", "password: 0xZZ\n", "", true},
		{"unquoted", "password: what\n", "", true},
		{"missing", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSecurityPassword([]byte(tt.stderr))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseSecurityPassword(%q) = %q, want %q", tt.stderr, got, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "what") {
				t.Errorf("error %q echoes the password line", err)
			}
		})
	}
}
