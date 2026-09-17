package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeTemp drops the shipped example config into a temp dir, which is exactly
// what a user has after running `callmqtt init`.
func writeTemp(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return path
}

// The comments in the shipped config are its documentation. Saving from the
// tray must not quietly delete them, or a user who toggles one checkbox is
// left with a file they no longer understand.
func TestSavePreservesComments(t *testing.T) {
	path := writeTemp(t, Example)

	before, err := Load(path)
	if err != nil {
		t.Fatalf("load example: %v", err)
	}

	s := before.Settings()
	s.BrokerHost = "10.0.0.5"
	s.Detectors["zoom"] = false
	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	// Spot-check the comments that carry the reasoning a user most needs.
	wantComments := []string{
		"Presence is published ONLY while connected",
		"Prefer an environment variable over a literal secret",
		"Deliberately asymmetric",
		"a light that lies is worse than one that is late",
		"Home Assistant gives up on the entity",
	}
	for _, want := range wantComments {
		if !strings.Contains(string(saved), want) {
			t.Errorf("saving deleted the comment %q:\n%s", want, saved)
		}
	}

	countBefore := strings.Count(string(Example), "#")
	countAfter := strings.Count(string(saved), "#")
	if countAfter < countBefore {
		t.Errorf("comment lines dropped from %d to %d", countBefore, countAfter)
	}
}

func TestSaveAppliesChanges(t *testing.T) {
	path := writeTemp(t, Example)

	before, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	s := before.Settings()
	s.BrokerHost = "10.0.0.5"
	s.BrokerPort = 8883
	s.Username = "sharon"
	s.Password = "hunter2"
	s.PasswordChanged = true
	s.Discovery = false
	s.Detectors["zoom"] = false
	s.Detectors["teams"] = true
	s.AllowedNetworks = []NetworkRule{{Name: "Loft", CIDRs: []string{"10.1.2.0/24"}}}

	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	after, err := Load(path)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"host", after.MQTT.Host, "10.0.0.5"},
		{"port", after.MQTT.Port, 8883},
		{"username", after.MQTT.Username, "sharon"},
		{"password", after.MQTT.Password, "hunter2"},
		{"discovery", after.MQTT.Discovery.Enabled, false},
		{"zoom enabled", after.Detectors["zoom"].Enabled, false},
		{"teams enabled", after.Detectors["teams"].Enabled, true},
		{"network count", len(after.AllowedNetworks), 1},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if after.AllowedNetworks[0].Name != "Loft" {
		t.Errorf("network name = %q, want Loft", after.AllowedNetworks[0].Name)
	}

	// Settings the UI does not expose must survive untouched.
	if after.Detection.ExitDebounceSeconds != before.Detection.ExitDebounceSeconds {
		t.Errorf("saving changed a field the UI does not expose: exit debounce %d -> %d",
			before.Detection.ExitDebounceSeconds, after.Detection.ExitDebounceSeconds)
	}
	if after.Poll.HeartbeatSeconds != before.Poll.HeartbeatSeconds {
		t.Errorf("saving changed the heartbeat: %d -> %d",
			before.Poll.HeartbeatSeconds, after.Poll.HeartbeatSeconds)
	}
}

// Saving repeatedly must converge, not accumulate junk.
func TestSaveIsIdempotent(t *testing.T) {
	path := writeTemp(t, Example)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := cfg.Settings()
	s.BrokerHost = "10.0.0.5"

	if err := Save(path, s); err != nil {
		t.Fatalf("first save: %v", err)
	}
	first, _ := os.ReadFile(path)

	if err := Save(path, s); err != nil {
		t.Fatalf("second save: %v", err)
	}
	second, _ := os.ReadFile(path)

	if string(first) != string(second) {
		t.Errorf("saving the same settings twice changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// A UI that can write a config the agent then refuses to load would be worse
// than no UI. Save validates first and leaves the good file in place.
func TestSaveRejectsInvalidSettingsAndLeavesFileIntact(t *testing.T) {
	path := writeTemp(t, Example)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	original, _ := os.ReadFile(path)

	s := cfg.Settings()
	s.BrokerHost = "" // required

	if err := Save(path, s); err == nil {
		t.Fatal("expected an empty broker host to be rejected")
	}

	current, _ := os.ReadFile(path)
	if string(current) != string(original) {
		t.Error("a rejected save must leave the existing config untouched")
	}
}

// Turning every detector off, or clearing the allow-list, are both things a
// user can do in a tray menu by accident. Neither may produce a file the
// agent cannot load.
func TestSaveRejectsConfigsThatCouldNeverWork(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"every detector disabled", func(s *Settings) {
			for name := range s.Detectors {
				s.Detectors[name] = false
			}
		}},
		{"allow-list emptied", func(s *Settings) { s.AllowedNetworks = nil }},
		{"port out of range", func(s *Settings) { s.BrokerPort = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, Example)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			s := cfg.Settings()
			tt.mutate(&s)

			if err := Save(path, s); err == nil {
				t.Error("expected rejection, but the config was saved")
			}
		})
	}
}

// A password typed into the UI may contain anything. It must survive a round
// trip exactly, and must not break the YAML.
func TestPasswordRoundTrip(t *testing.T) {
	awkward := []string{
		"hunter2",
		"p@ss w0rd",
		`"quoted"`,
		"with: colon",
		"#notacomment",
		"tab\tand\\backslash",
		"emoji 🔐",
		"${NOT_EXPANDED}",
	}

	for _, want := range awkward {
		t.Run(want, func(t *testing.T) {
			path := writeTemp(t, Example)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			s := cfg.Settings()
			s.Password = want
			s.PasswordChanged = true
			if err := Save(path, s); err != nil {
				t.Fatalf("save: %v", err)
			}

			after, err := Load(path)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}

			// ${VAR} is expanded on load by design, so that one case is
			// checked against the raw file rather than the parsed value.
			if want == "${NOT_EXPANDED}" {
				raw, _ := os.ReadFile(path)
				if !strings.Contains(string(raw), "${NOT_EXPANDED}") {
					t.Errorf("an environment reference was not written literally:\n%s", raw)
				}
				return
			}
			if after.MQTT.Password != want {
				t.Errorf("password round-tripped as %q, want %q", after.MQTT.Password, want)
			}
		})
	}
}

// A save that never touched the password (toggling a detector, editing the
// allow-list, ...) must not turn a ${VAR} reference into the literal secret
// it currently expands to. Settings() always carries the expanded value so a
// dialog can pre-fill it; without PasswordChanged gating the write, every
// unrelated tray click would leak the secret into the file.
func TestSaveLeavesEnvPasswordAloneWhenUnchanged(t *testing.T) {
	t.Setenv("CALLMQTT_TEST_PASSWORD", "s3cret")
	path := writeTemp(t, []byte(minimal+"  password: \"${CALLMQTT_TEST_PASSWORD}\"\n"+
		"detectors:\n  teams: {enabled: true}\n  zoom: {enabled: true}\n  slack: {enabled: true}\n"))

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MQTT.Password != "s3cret" {
		t.Fatalf("password = %q, want the expanded value", cfg.MQTT.Password)
	}

	s := cfg.Settings()
	s.Detectors["zoom"] = false // an unrelated change; PasswordChanged stays false

	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(raw), "${CALLMQTT_TEST_PASSWORD}") {
		t.Errorf("save rewrote the env reference; file no longer contains it:\n%s", raw)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Errorf("save leaked the expanded secret into the file:\n%s", raw)
	}

	after, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.PasswordIsLiteral() {
		t.Error("password should still be reported as env-sourced after an unrelated save")
	}
}

// The counterpart to the test above: when a save is actually a
// dialog-driven password change, the new literal must be written and
// PasswordIsLiteral must flip to true on reload.
func TestSaveWritesPasswordWhenChanged(t *testing.T) {
	t.Setenv("CALLMQTT_TEST_PASSWORD", "s3cret")
	path := writeTemp(t, []byte(minimal+"  password: \"${CALLMQTT_TEST_PASSWORD}\"\n"))

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	s := cfg.Settings()
	s.Password = "new-literal-secret"
	s.PasswordChanged = true

	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	after, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.MQTT.Password != "new-literal-secret" {
		t.Errorf("password = %q, want the new literal", after.MQTT.Password)
	}
	if !after.PasswordIsLiteral() {
		t.Error("a dialog-written password must be reported as literal")
	}
}

// The saved file holds a plaintext password, so it must not be world-readable.
//
// Windows is exempt because it does not model Unix permission bits at all:
// os.Chmod there only toggles the read-only attribute. Protection on Windows
// comes from the config living inside the user's %AppData%, which other
// standard users cannot read.
func TestSavedConfigIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows does not model unix permission bits; %AppData% provides the restriction")
	}

	path := writeTemp(t, Example)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	s := cfg.Settings()
	s.Password = "hunter2"
	s.PasswordChanged = true
	if err := Save(path, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("config mode is %v, want no group or other access", mode)
	}
}
