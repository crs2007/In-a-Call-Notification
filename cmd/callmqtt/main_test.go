package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
