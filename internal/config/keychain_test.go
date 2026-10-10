package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeKeychain installs a KeychainLookup holding items keyed "service/account"
// and records every lookup. Tests using it must not run in parallel.
func fakeKeychain(t *testing.T, items map[string]string, fail error) *[]string {
	t.Helper()
	var calls []string
	prev := KeychainLookup
	KeychainLookup = func(service, account string) (string, error) {
		calls = append(calls, service+"/"+account)
		if fail != nil {
			return "", fail
		}
		secret, ok := items[service+"/"+account]
		if !ok {
			return "", fmt.Errorf("lookup: %w", ErrKeychainItemNotFound)
		}
		return secret, nil
	}
	t.Cleanup(func() { KeychainLookup = prev })
	return &calls
}

func TestKeychainReferenceValidation(t *testing.T) {
	tests := []struct {
		name     string
		mqtt     string // appended under mqtt:
		noLookup bool   // as on every OS but macOS
		want     []string
	}{
		{"valid", "  username: bob\n  password: keychain:callmqtt\n", false, nil},
		{"service is trimmed", "  username: bob\n  password: \"keychain: callmqtt \"\n", false, nil},
		{"not macOS", "  username: bob\n  password: keychain:callmqtt\n", true, []string{"keychain: references are only supported on macOS"}},
		{"no service and no username, both reported", "  password: \"keychain:\"\n", false, []string{"names no keychain service", "mqtt.username is required"}},
		{"env var expanding to keychain: is a literal", "  username: bob\n  password: ${CALLMQTT_TEST_KC}\n", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CALLMQTT_TEST_KC", "keychain:callmqtt")
			calls := fakeKeychain(t, nil, nil)
			if tt.noLookup {
				KeychainLookup = nil
			}
			_, err := Parse([]byte(minimal + tt.mqtt))
			if len(tt.want) == 0 && err != nil {
				t.Fatalf("Parse: %v", err)
			}
			for _, w := range tt.want {
				if err == nil || !strings.Contains(err.Error(), w) {
					t.Errorf("error %v does not mention %q", err, w)
				}
			}
			if len(*calls) != 0 {
				t.Errorf("Parse looked up %q; only Load may touch the keychain", *calls)
			}
		})
	}
}

func TestKeychainReferenceIsNotLiteralAndIsRedacted(t *testing.T) {
	fakeKeychain(t, map[string]string{"callmqtt/bob": "s3cret"}, nil)
	path := writeTemp(t, []byte(minimal+"  username: bob\n  password: keychain:callmqtt\n"))

	for _, stage := range []string{"parsed", "loaded"} {
		var cfg *Config
		var err error
		if stage == "parsed" {
			raw, _ := os.ReadFile(path)
			cfg, err = Parse(raw)
		} else {
			cfg, err = Load(path)
		}
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if cfg.PasswordIsLiteral() {
			t.Errorf("%s: a keychain reference must not be reported as a literal password", stage)
		}
		if got := cfg.Redacted().MQTT.Password; got != "********" {
			t.Errorf("%s: Redacted password = %q, want ********", stage, got)
		}
	}
}

func TestLoadResolvesKeychainPassword(t *testing.T) {
	tests := []struct {
		name     string
		items    map[string]string
		fail     error
		password string
		want     string   // resolved password on success
		wantErr  []string // substrings of the error
	}{
		{"found", map[string]string{"callmqtt/bob": "s3cret"}, nil, "keychain:callmqtt", "s3cret", nil},
		{"other service", map[string]string{"broker-home/bob": "x#y: z"}, nil, "keychain:broker-home", "x#y: z", nil},
		{"missing item names the fix", nil, nil, "keychain:callmqtt", "", []string{
			"no keychain item for service callmqtt account bob",
			"`security add-generic-password -U -s callmqtt -a bob -w`",
		}},
		{"lookup failure is wrapped", nil, errors.New("user interaction is not allowed"), "keychain:callmqtt", "", []string{
			"keychain service callmqtt account bob", "user interaction is not allowed",
		}},
		{"empty secret fails closed", map[string]string{"callmqtt/bob": ""}, nil, "keychain:callmqtt", "", []string{"empty password"}},
		{"literal password never looks up", nil, nil, "plain", "plain", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := fakeKeychain(t, tt.items, tt.fail)
			path := writeTemp(t, []byte(minimal+"  username: bob\n  password: \""+tt.password+"\"\n"))

			cfg, err := Load(path)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if cfg.MQTT.Password != tt.want {
					t.Errorf("password = %q, want %q", cfg.MQTT.Password, tt.want)
				}
			}
			for _, w := range tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), w) {
					t.Errorf("error %v does not mention %q", err, w)
				}
			}
			wantCalls := 1
			if !strings.HasPrefix(tt.password, keychainPrefix) {
				wantCalls = 0
			}
			if len(*calls) != wantCalls {
				t.Errorf("lookups = %q, want %d", *calls, wantCalls)
			}
		})
	}
}

// TestSaveKeepsKeychainReference: the tray saves Settings taken from a
// loaded config, whose password is the resolved secret. An unrelated change
// must leave the reference in the file, not write the secret out.
func TestSaveKeepsKeychainReference(t *testing.T) {
	calls := fakeKeychain(t, map[string]string{"callmqtt/bob": "s3cret"}, nil)
	path := writeTemp(t, []byte(minimal+"  username: bob\n  password: keychain:callmqtt\n"))

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := cfg.Settings()
	s.Detectors["zoom"] = false
	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(*calls) != 1 {
		t.Errorf("lookups = %q; Save must not touch the keychain", *calls)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "keychain:callmqtt") || strings.Contains(string(raw), "s3cret") {
		t.Errorf("save did not keep the keychain reference:\n%s", raw)
	}
	after, err := Load(path)
	if err != nil || after.MQTT.Password != "s3cret" {
		t.Errorf("reload = %v, %v; want the secret resolved again", after, err)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"callmqtt", "callmqtt"},
		{"bob@home.lan", "bob@home.lan"},
		{"two words", "'two words'"},
		{"o'brien", `'o'\''brien'`},
		{"", "''"},
		{"$HOME", "'$HOME'"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
