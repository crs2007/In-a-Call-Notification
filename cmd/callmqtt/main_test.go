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

func TestRotatingWriter(t *testing.T) {
	t.Run("rotates mid-process without a restart", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "callmqtt.log")
		w, err := newRotatingWriter(path)
		if err != nil {
			t.Fatalf("newRotatingWriter: %v", err)
		}
		defer w.Close()

		chunk := []byte(strings.Repeat("x", 1024*1024))
		for i := 0; i < 6; i++ {
			if _, err := w.Write(chunk); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}

		if _, err := os.Stat(path + ".1"); err != nil {
			t.Fatalf("expected rotated generation to exist: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat current log: %v", err)
		}
		if info.Size() >= maxLogSize {
			t.Fatalf("current log is %d bytes, want it rotated back below maxLogSize", info.Size())
		}
	})

	t.Run("seeds size from an existing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "callmqtt.log")
		existing := strings.Repeat("x", maxLogSize-10)
		if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
		w, err := newRotatingWriter(path)
		if err != nil {
			t.Fatalf("newRotatingWriter: %v", err)
		}
		defer w.Close()

		if _, err := w.Write([]byte(strings.Repeat("y", 20))); err != nil {
			t.Fatalf("write: %v", err)
		}

		if _, err := os.Stat(path + ".1"); err != nil {
			t.Fatalf("expected rotation once the seeded size plus the new write crossed maxLogSize: %v", err)
		}
	})
}
