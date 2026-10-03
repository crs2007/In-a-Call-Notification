package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crs2007/callmqtt/internal/config"
)

func TestAbsolutePath(t *testing.T) {
	t.Run("empty optional path", func(t *testing.T) {
		got, err := absolutePath("")
		if err != nil {
			t.Fatalf("absolutePath: %v", err)
		}
		if got != "" {
			t.Fatalf("absolutePath(\"\") = %q, want empty", got)
		}
	})

	t.Run("relative path", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		got, err := absolutePath(filepath.Join("logs", "callmqtt.log"))
		if err != nil {
			t.Fatalf("absolutePath: %v", err)
		}
		want := filepath.Join(dir, "logs", "callmqtt.log")
		if got != want {
			t.Fatalf("absolutePath() = %q, want %q", got, want)
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "config.yaml")
		got, err := absolutePath(want)
		if err != nil {
			t.Fatalf("absolutePath: %v", err)
		}
		if got != want {
			t.Fatalf("absolutePath() = %q, want %q", got, want)
		}
	})
}

func TestRotateLogIfLarge(t *testing.T) {
	t.Run("missing file is not an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "callmqtt.log")
		if err := rotateLogIfLarge(path); err != nil {
			t.Fatalf("rotateLogIfLarge: %v", err)
		}
	})

	t.Run("small file is left alone", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "callmqtt.log")
		if err := os.WriteFile(path, []byte("a few bytes"), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
		if err := rotateLogIfLarge(path); err != nil {
			t.Fatalf("rotateLogIfLarge: %v", err)
		}
		if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
			t.Fatalf("rotated file exists for a small log")
		}
	})

	t.Run("oversized file is rotated, overwriting any previous generation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "callmqtt.log")
		big := strings.Repeat("x", maxLogSize+1)
		if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
		if err := os.WriteFile(path+".1", []byte("stale generation"), 0o644); err != nil {
			t.Fatalf("write stale rotated log: %v", err)
		}

		if err := rotateLogIfLarge(path); err != nil {
			t.Fatalf("rotateLogIfLarge: %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("original path still exists after rotation")
		}
		rotated, err := os.ReadFile(path + ".1")
		if err != nil {
			t.Fatalf("read rotated log: %v", err)
		}
		if string(rotated) != big {
			t.Fatalf("rotated log has %d bytes, want the original %d-byte contents", len(rotated), len(big))
		}
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("missing config on a plain launch writes the starter", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "callmqtt", "config.yaml")

		cfg, err := loadConfig(path, true)
		if cfg != nil || err == nil {
			t.Fatalf("loadConfig = (%v, %v), want (nil, error)", cfg, err)
		}
		for _, want := range []string{"starter", path, "mqtt.host", "allowed_networks", appTitle} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("starter config was not written: %v", readErr)
		}
		if string(got) != string(config.Example) {
			t.Fatal("starter config differs from the packaged example")
		}
	})

	t.Run("missing config under an inspection flag writes nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")

		_, err := loadConfig(path, false)
		if err == nil {
			t.Fatal("loadConfig succeeded on a missing file")
		}
		if !strings.Contains(err.Error(), "callmqtt init") || !strings.Contains(err.Error(), path) {
			t.Errorf("error %q should point at `callmqtt init` and the path", err)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("a file appeared at %s (stat: %v)", path, statErr)
		}
	})

	t.Run("existing config loads and is left alone", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, config.Example, 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := loadConfig(path, true)
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if cfg == nil || cfg.MQTT.Host == "" {
			t.Fatalf("loaded config is empty: %+v", cfg)
		}
	})

	t.Run("unreadable config is not mistaken for a missing one", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("mqtt: [not a mapping"), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := loadConfig(path, true)
		if err == nil || strings.Contains(err.Error(), "starter") {
			t.Fatalf("broken config: got %v, want a parse error, not a bootstrap", err)
		}
	})
}
