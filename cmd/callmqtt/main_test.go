package main

import (
	"path/filepath"
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
