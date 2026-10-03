package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileRotatesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "callmqtt.log")
	const limit = 1024
	r, err := newRotatingFile(path, limit)
	if err != nil {
		t.Fatalf("newRotatingFile: %v", err)
	}
	defer r.Close()

	line := strings.Repeat("x", 99) + "\n"
	total := 0
	for i := 0; i < 50; i++ { // 5000 bytes, several times the limit
		n, err := r.Write([]byte(line))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		total += n
	}

	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	old, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("rotation did not happen: %v", err)
	}
	if len(live) > limit || len(old) > limit {
		t.Fatalf("live=%d old=%d bytes, want each <= %d", len(live), len(old), limit)
	}
	if len(live)+len(old) > total || len(live) == 0 {
		t.Fatalf("unexpected sizes live=%d old=%d total=%d", len(live), len(old), total)
	}
}

func TestRotatingFileAppendsToExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "callmqtt.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 900)), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := newRotatingFile(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Write([]byte(strings.Repeat("b", 200))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("pre-existing size was not counted: %v", err)
	}
}
