//go:build darwin

package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/crs2007/callmqtt/internal/config"
	platformdarwin "github.com/crs2007/callmqtt/platform/darwin"
)

// TestIsTerminal covers what a Finder- or launchd-started app is handed
// (/dev/null) and what a pipe looks like; neither is a terminal. A real
// terminal is only checked when the test itself has one.
func TestIsTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	tests := []struct {
		name string
		f    *os.File
		want bool
	}{
		{"/dev/null", devNull, false},
		{"pipe read end", r, false},
		{"pipe write end", w, false},
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		tests = append(tests, struct {
			name string
			f    *os.File
			want bool
		}{"controlling terminal", tty, true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTerminal(tt.f.Fd()); got != tt.want {
				t.Errorf("isTerminal(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDarwinPasswordAdvice(t *testing.T) {
	for _, want := range []string{`"keychain:callmqtt"`, "security add-generic-password -U -s callmqtt -a"} {
		if !strings.Contains(passwordAdvice, want) {
			t.Errorf("init advice does not mention %q", want)
		}
	}
	if strings.Contains(passwordAdvice, "setx") {
		t.Error("init advice tells a Mac user to run setx")
	}
}

func TestKeychainLookup(t *testing.T) {
	if config.KeychainLookup == nil {
		t.Fatal("config.KeychainLookup is not wired on macOS")
	}
	boom := errors.New("user interaction is not allowed")
	tests := []struct {
		name       string
		secret     string
		err        error
		want       string
		wantErr    error
		notMissing bool
	}{
		{"found", "s3cret", nil, "s3cret", nil, false},
		{"missing maps to config's sentinel", "", platformdarwin.ErrKeychainItemNotFound, "", config.ErrKeychainItemNotFound, false},
		{"other errors pass through", "", boom, "", boom, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := readKeychain
			t.Cleanup(func() { readKeychain = prev })
			readKeychain = func(service, account string) (string, error) {
				if service != "callmqtt" || account != "bob" {
					t.Errorf("looked up %s/%s, want callmqtt/bob", service, account)
				}
				return tt.secret, tt.err
			}
			got, err := keychainLookup("callmqtt", "bob")
			if got != tt.want || !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Errorf("keychainLookup = %q, %v; want %q, %v", got, err, tt.want, tt.wantErr)
			}
			if tt.notMissing && errors.Is(err, config.ErrKeychainItemNotFound) {
				t.Errorf("%v must not read as a missing item", err)
			}
		})
	}
}
