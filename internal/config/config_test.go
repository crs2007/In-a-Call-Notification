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

	if len(cfg.EnabledDetectors()) != 4 {
		t.Errorf("expected teams, zoom, slack and meet enabled by default, got %v", cfg.EnabledDetectors())
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
	got := expandField("test", "Microsoft Teams$HOME")
	if !strings.Contains(got, "$HOME") {
		t.Errorf("bare $NAME must be left alone, got %q", got)
	}
}

// Expansion must happen on the already-parsed string, not on the YAML
// source: substituting into the source let a password containing '#' get
// truncated as a YAML comment before it ever reached the parser.
func TestReproHashInPassword(t *testing.T) {
	t.Setenv("CALLMQTT_TEST_PASSWORD", "hunter2 #2024")

	cfg, err := Parse([]byte(minimal + "  password: \"${CALLMQTT_TEST_PASSWORD}\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.MQTT.Password != "hunter2 #2024" {
		t.Errorf("password = %q, want the env value intact", cfg.MQTT.Password)
	}
}

// The same class of bug: a newline in the env value must not be able to
// inject a YAML key or otherwise change how the rest of the document parses.
func TestReproNewlineInPassword(t *testing.T) {
	t.Setenv("CALLMQTT_TEST_PASSWORD", "line1\nlogging:\n  level: debug")

	cfg, err := Parse([]byte(minimal + "  password: \"${CALLMQTT_TEST_PASSWORD}\"\nlogging:\n  level: info\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.MQTT.Password != "line1\nlogging:\n  level: debug" {
		t.Errorf("password = %q, want the env value intact", cfg.MQTT.Password)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("logging.level = %q, want the value the file actually set (an injected key must not win)", cfg.Logging.Level)
	}
}

// An unset ${VAR} must still resolve to empty (Validate reports it, same as
// before) rather than becoming an error in its own right — env-var typos
// shouldn't need a different failure mode than a missing field.
func TestUnsetEnvVarExpandsEmpty(t *testing.T) {
	os.Unsetenv("CALLMQTT_DOES_NOT_EXIST")

	cfg, err := Parse([]byte(minimal + "  password: \"${CALLMQTT_DOES_NOT_EXIST}\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.MQTT.Password != "" {
		t.Errorf("password = %q, want empty for an unset variable", cfg.MQTT.Password)
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
			name:      "poll interval above the upper bound",
			yaml:      minimal + "poll:\n  detect_seconds: 86401\n",
			wantError: "poll.detect_seconds must be at most 86400",
		},
		{
			name:      "network interval above the upper bound",
			yaml:      minimal + "poll:\n  network_seconds: 86401\n",
			wantError: "poll.network_seconds must be at most 86400",
		},
		{
			name:      "heartbeat interval above the upper bound",
			yaml:      minimal + "poll:\n  heartbeat_seconds: 86401\n",
			wantError: "poll.heartbeat_seconds must be at most 86400",
		},
		{
			// The issue #10 repro: seconds values this large overflow the
			// time.Duration multiplication in Poll.Detect/Heartbeat, which
			// used to pass validation (only a lower bound was enforced) and
			// then panic in time.NewTicker at startup.
			name:      "poll seconds large enough to overflow time.Duration",
			yaml:      minimal + "poll:\n  detect_seconds: 10000000000\n  heartbeat_seconds: 20000000000\n",
			wantError: "poll.detect_seconds must be at most 86400",
		},
		{
			name:      "enter debounce above the upper bound",
			yaml:      minimal + "detection:\n  enter_debounce_seconds: 3601\n",
			wantError: "detection.enter_debounce_seconds must be at most 3600",
		},
		{
			name:      "exit debounce above the upper bound",
			yaml:      minimal + "detection:\n  exit_debounce_seconds: 3601\n",
			wantError: "detection.exit_debounce_seconds must be at most 3600",
		},
		{
			name:      "unknown log level",
			yaml:      minimal + "logging:\n  level: chatty\n",
			wantError: "logging.level",
		},
		{
			name:      "every detector disabled",
			yaml:      minimal + "detectors:\n  teams: {enabled: false}\n  zoom: {enabled: false}\n  slack: {enabled: false}\n  meet: {enabled: false}\n",
			wantError: "no detectors are enabled",
		},
		{
			name:      "typo'd detector app name",
			yaml:      minimal + "detectors:\n  team: {enabled: true}\n",
			wantError: "detectors.team does not match a known detection rule",
		},
		{
			name:      "device_id slugifies to empty",
			yaml:      minimal + "app:\n  device_id: \"###\"\n",
			wantError: "device_id resolves to empty",
		},
		{
			// Each field individually respects its own >= 1 minimum, but
			// heartbeat_seconds is tiny relative to detect_seconds: 2*1.5=3
			// is less than 5*2=10, so the expire_after window could lapse
			// between detect cycles.
			name:      "heartbeat too low relative to detect interval",
			yaml:      minimal + "poll:\n  detect_seconds: 5\n  heartbeat_seconds: 2\n",
			wantError: "poll.heartbeat_seconds (2) is too low relative to poll.detect_seconds (5)",
		},
		{
			name: "tls cert_file without key_file",
			yaml: "mqtt:\n  host: broker\n  tls:\n    cert_file: client.pem\n" +
				"allowed_networks:\n  - {name: Home, cidrs: [\"10.0.0.0/8\"]}\n",
			wantError: "mqtt.tls.cert_file and mqtt.tls.key_file must both be set, or both left empty",
		},
		{
			name: "tls key_file without cert_file",
			yaml: "mqtt:\n  host: broker\n  tls:\n    key_file: client-key.pem\n" +
				"allowed_networks:\n  - {name: Home, cidrs: [\"10.0.0.0/8\"]}\n",
			wantError: "mqtt.tls.cert_file and mqtt.tls.key_file must both be set, or both left empty",
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

// TestHeartbeatDetectBoundaryAccepted pins the exact boundary of the 7.6
// invariant: heartbeat_seconds*1.5 == detect_seconds*2 must be accepted (the
// check is strictly-less, not less-or-equal), and the defaults comfortably
// clear it.
func TestHeartbeatDetectBoundaryAccepted(t *testing.T) {
	// 4*1.5 == 3*2 == 6: right at the boundary, must not be rejected.
	cfg, err := Parse([]byte(minimal + "poll:\n  detect_seconds: 3\n  heartbeat_seconds: 4\n"))
	if err != nil {
		t.Fatalf("boundary heartbeat/detect ratio rejected: %v", err)
	}
	if cfg.Poll.HeartbeatSeconds != 4 || cfg.Poll.DetectSeconds != 3 {
		t.Fatalf("unexpected poll config: %+v", cfg.Poll)
	}

	// The shipped defaults (detect=2, heartbeat=60) must also stay well
	// clear of the boundary.
	if err := Defaults().Validate(); err == nil {
		t.Fatal("Defaults() alone is missing required fields and should fail Validate for other reasons")
	} else if strings.Contains(err.Error(), "too low relative to") {
		t.Errorf("default poll settings must not trip the heartbeat/detect invariant: %v", err)
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

// TestTLSCertAndKeyBothSetIsAccepted is the accept-side counterpart to the
// "cert_file without key_file"/"key_file without cert_file" rejections in
// TestValidationRejects: both set together is a valid, common configuration
// (mutual TLS) and must not trip the same cross-field check.
func TestTLSCertAndKeyBothSetIsAccepted(t *testing.T) {
	cfg, err := Parse([]byte(minimal + "  tls:\n    cert_file: client.pem\n    key_file: client-key.pem\n"))
	if err != nil {
		t.Fatalf("cert_file and key_file set together should be valid: %v", err)
	}
	if cfg.MQTT.TLS.CertFile != "client.pem" || cfg.MQTT.TLS.KeyFile != "client-key.pem" {
		t.Errorf("tls cert/key = %q/%q, want client.pem/client-key.pem", cfg.MQTT.TLS.CertFile, cfg.MQTT.TLS.KeyFile)
	}
}

// TestTLSFieldsParsed confirms ca_file, cert_file and key_file round-trip
// through Parse unresolved (Parse has no config path to resolve them
// against — that is Load's job, covered by TestLoadResolvesTLSPathsRelativeToConfigDir).
func TestTLSFieldsParsed(t *testing.T) {
	yaml := minimal + "  tls:\n    enabled: true\n    ca_file: ca.pem\n" +
		"    cert_file: client.pem\n    key_file: client-key.pem\n"
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.MQTT.TLS.Enabled {
		t.Error("tls.enabled = false, want true")
	}
	if cfg.MQTT.TLS.CAFile != "ca.pem" {
		t.Errorf("tls.ca_file = %q, want ca.pem", cfg.MQTT.TLS.CAFile)
	}
	if cfg.MQTT.TLS.CertFile != "client.pem" {
		t.Errorf("tls.cert_file = %q, want client.pem", cfg.MQTT.TLS.CertFile)
	}
	if cfg.MQTT.TLS.KeyFile != "client-key.pem" {
		t.Errorf("tls.key_file = %q, want client-key.pem", cfg.MQTT.TLS.KeyFile)
	}
}

// TestLoadResolvesTLSPathsRelativeToConfigDir is the 7.5 repro: a relative
// ca_file/cert_file/key_file must resolve against the directory holding
// config.yaml, not the process's working directory — the same convention
// cmd/callmqtt already applies to rules_file (see loadRules). An already
// absolute path must be left alone.
func TestLoadResolvesTLSPathsRelativeToConfigDir(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + string(os.PathSeparator) + "config.yaml"

	absKey := dir + string(os.PathSeparator) + "somewhere-else" + string(os.PathSeparator) + "client-key.pem"
	body := minimal + "  tls:\n    ca_file: ca.pem\n    cert_file: certs/client.pem\n    key_file: " + absKey + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	wantCA := dir + string(os.PathSeparator) + "ca.pem"
	if cfg.MQTT.TLS.CAFile != wantCA {
		t.Errorf("tls.ca_file = %q, want %q (relative to config dir)", cfg.MQTT.TLS.CAFile, wantCA)
	}
	wantCert := dir + string(os.PathSeparator) + "certs" + string(os.PathSeparator) + "client.pem"
	if cfg.MQTT.TLS.CertFile != wantCert {
		t.Errorf("tls.cert_file = %q, want %q (relative to config dir)", cfg.MQTT.TLS.CertFile, wantCert)
	}
	if cfg.MQTT.TLS.KeyFile != absKey {
		t.Errorf("tls.key_file = %q, want %q (an absolute path must be left alone)", cfg.MQTT.TLS.KeyFile, absKey)
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
		// 7.7 repro: a long run of nothing but separators must collapse to
		// nothing, not a long run of hyphens, and must not be quadratically
		// slow to produce (see BenchmarkSlugify).
		{strings.Repeat("!", 10000), ""},
		// A separator run in the middle of two alnum stretches still
		// collapses to exactly one hyphen.
		{"a" + strings.Repeat("!", 500) + "b", "a-b"},
	}
	for _, tt := range tests {
		got := slugify(tt.in)
		want := tt.want
		if got != want {
			// Truncate long inputs/outputs in the failure message so it stays
			// readable.
			t.Errorf("slugify(%.40q...) = %.40q, want %.40q", tt.in, got, want)
		}
	}
}

// BenchmarkSlugify demonstrates the 7.7 fix: slugifying a long run of
// separator characters must be linear, not quadratic, in input length. Run
// with `go test ./internal/config/... -bench=Slugify -benchtime=1x` — there
// is no CI gate on the result, but it should complete essentially instantly
// even at this size, where the old strings.Builder.String()-per-rune
// implementation would have been visibly slow.
func BenchmarkSlugify(b *testing.B) {
	input := strings.Repeat("!", 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slugify(input)
	}
}
