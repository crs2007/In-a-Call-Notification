package config

import (
	"os"
	"strings"
	"testing"
)

// minimal is the smallest configuration that should be accepted. If this ever
// stops working, the defaults have regressed and every user's first-run
// experience got worse.
const minimal = `
allowed_networks:
  - name: Home
    cidrs: ["192.168.1.0/24"]
mqtt:
  host: 192.168.1.10
`

func TestParseMinimalConfigAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("minimal config rejected: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"mqtt.port", cfg.MQTT.Port, 1883},
		{"mqtt.qos", cfg.MQTT.QoS, byte(1)},
		{"mqtt.retain", cfg.MQTT.Retain, true},
		{"discovery.enabled", cfg.MQTT.Discovery.Enabled, true},
		{"discovery.prefix", cfg.MQTT.Discovery.Prefix, "homeassistant"},
		{"poll.detect_seconds", cfg.Poll.DetectSeconds, 2},
		{"poll.heartbeat_seconds", cfg.Poll.HeartbeatSeconds, 60},
		{"active_threshold", cfg.Detection.ActiveThreshold, 0.70},
		{"enter_debounce", cfg.Detection.EnterDebounceSeconds, 2},
		{"exit_debounce", cfg.Detection.ExitDebounceSeconds, 8},
		{"logging.level", cfg.Logging.Level, "info"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	if len(cfg.EnabledDetectors()) != 3 {
		t.Errorf("expected teams, zoom and slack enabled by default, got %v", cfg.EnabledDetectors())
	}
}

func TestExitDebounceExceedsEnterDebounce(t *testing.T) {
	// The asymmetry is deliberate and load-bearing: it is what stops a camera
	// toggle or a brief reconnect from flickering the light.
	d := Defaults().Detection
	if d.ExitDebounce() <= d.EnterDebounce() {
		t.Errorf("exit debounce %v must exceed enter debounce %v", d.ExitDebounce(), d.EnterDebounce())
	}
}

func TestDeviceIDSubstitutedIntoTopics(t *testing.T) {
	cfg, err := Parse([]byte(minimal + "app:\n  device_id: Sharon-PC\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := "desktop-presence/sharon-pc/call"; cfg.Topics.State != want {
		t.Errorf("topics.state = %q, want %q", cfg.Topics.State, want)
	}
	if want := "callmqtt-sharon-pc"; cfg.MQTT.ClientID != want {
		t.Errorf("mqtt.client_id = %q, want %q", cfg.MQTT.ClientID, want)
	}
}

func TestAutoDeviceIDIsResolved(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.App.DeviceID == "" || cfg.App.DeviceID == "auto" {
		t.Errorf("device_id %q was not resolved from the hostname", cfg.App.DeviceID)
	}
	if strings.Contains(cfg.Topics.State, "{device_id}") {
		t.Errorf("topic placeholder left unresolved: %q", cfg.Topics.State)
	}
}

func TestEnvExpansion(t *testing.T) {
	t.Setenv("CALLMQTT_TEST_PASSWORD", "s3cret")

	cfg, err := Parse([]byte(minimal + "  password: \"${CALLMQTT_TEST_PASSWORD}\"\n  username: bob\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.MQTT.Password != "s3cret" {
		t.Errorf("password = %q, want the expanded value", cfg.MQTT.Password)
	}
	if cfg.PasswordIsLiteral() {
		t.Error("an expanded ${VAR} password should not be reported as literal")
	}
	if cfg.Redacted().MQTT.Password != "********" {
		t.Error("Redacted() must mask the password")
	}
}

func TestBareDollarIsNotExpanded(t *testing.T) {
	// Detection rules are regexes; $ is an anchor, not a variable.
	t.Setenv("HOME", "/should-not-appear")
	got := string(expandEnv([]byte(`regex: "Microsoft Teams$HOME"`)))
	if !strings.Contains(got, "$HOME") {
		t.Errorf("bare $NAME must be left alone, got %q", got)
	}
}

func TestValidationRejects(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantError string
	}{
		{
			name:      "missing broker host",
			yaml:      "allowed_networks:\n  - {name: Home, cidrs: [\"10.0.0.0/8\"]}\n",
			wantError: "mqtt.host is required",
		},
		{
			name:      "no allowed networks means nothing is ever published",
			yaml:      "mqtt:\n  host: broker\n",
			wantError: "allowed_networks is empty",
		},
		{
			name:      "malformed cidr",
			yaml:      "mqtt:\n  host: broker\nallowed_networks:\n  - {name: Home, cidrs: [\"192.168.1.0/33\"]}\n",
			wantError: "invalid cidr",
		},
		{
			name:      "malformed gateway",
			yaml:      "mqtt:\n  host: broker\nallowed_networks:\n  - {name: Home, gateways: [\"not-an-ip\"]}\n",
			wantError: "invalid gateway",
		},
		{
			name:      "rule with nothing to match on",
			yaml:      "mqtt:\n  host: broker\nallowed_networks:\n  - {name: Home}\n",
			wantError: "no ssids, bssids, cidrs or gateways",
		},
		{
			name:      "unnamed rule",
			yaml:      "mqtt:\n  host: broker\nallowed_networks:\n  - {cidrs: [\"10.0.0.0/8\"]}\n",
			wantError: "name is required",
		},
		{
			name:      "port out of range",
			yaml:      minimal + "  port: 99999\n",
			wantError: "out of range",
		},
		{
			name:      "inactive threshold above active",
			yaml:      minimal + "detection:\n  active_threshold: 0.3\n  inactive_threshold: 0.8\n",
			wantError: "must be below active_threshold",
		},
		{
			name:      "zero poll interval",
			yaml:      minimal + "poll:\n  detect_seconds: 0\n",
			wantError: "poll.detect_seconds must be at least 1",
		},
		{
			name:      "unknown log level",
			yaml:      minimal + "logging:\n  level: chatty\n",
			wantError: "logging.level",
		},
		{
			name:      "every detector disabled",
			yaml:      minimal + "detectors:\n  teams: {enabled: false}\n  zoom: {enabled: false}\n  slack: {enabled: false}\n",
			wantError: "no detectors are enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("expected rejection mentioning %q, got none", tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("error %q does not mention %q", err, tt.wantError)
			}
		})
	}
}

func TestValidationReportsEveryProblemAtOnce(t *testing.T) {
	// A user with three mistakes should learn about all three in one run.
	_, err := Parse([]byte("mqtt:\n  port: 0\nlogging:\n  level: loud\n"))
	if err == nil {
		t.Fatal("expected rejection")
	}
	for _, want := range []string{"mqtt.host is required", "mqtt.port", "allowed_networks is empty", "logging.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("combined error is missing %q:\n%v", want, err)
		}
	}
}

func TestLiteralPasswordIsFlagged(t *testing.T) {
	cfg, err := Parse([]byte(minimal + "  password: hunter2\n"))
	if err != nil {
		t.Fatalf("a literal password is a warning, not an error: %v", err)
	}
	if !cfg.PasswordIsLiteral() {
		t.Error("a password written into the file should be reported as literal")
	}
}

// The shipped example must never parse into a working config that happens to
// point at a real broker. With CALLMQTT_MQTT_PASSWORD unset, the ${VAR}
// reference in example.yaml should expand to empty, not leak a literal
// secret — this is the guard against the mqcommunicator leak recurring.
func TestExampleConfigHasNoWorkingCredentials(t *testing.T) {
	if orig, ok := os.LookupEnv("CALLMQTT_MQTT_PASSWORD"); ok {
		os.Unsetenv("CALLMQTT_MQTT_PASSWORD")
		t.Cleanup(func() { os.Setenv("CALLMQTT_MQTT_PASSWORD", orig) })
	}

	cfg, err := Parse(Example)
	if err != nil {
		t.Fatalf("example.yaml should still parse: %v", err)
	}
	if cfg.PasswordIsLiteral() {
		t.Error("example.yaml's password must reference an env var, not a literal")
	}
	if cfg.MQTT.Password != "" {
		t.Errorf("mqtt.password = %q, want empty with the env var unset", cfg.MQTT.Password)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Sharon-PC", "sharon-pc"},
		{"DESKTOP_ABC123", "desktop-abc123"},
		{"my pc.local", "my-pc-local"},
		{"--weird--", "weird"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := slugify(tt.in); got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
