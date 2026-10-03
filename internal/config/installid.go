package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// userConfigDir is a variable so tests can keep the install id out of the real
// user profile.
var userConfigDir = os.UserConfigDir

// installSuffix returns a short identifier that is stable for this install and
// differs between installs, so two machines (or two sessions) that share a
// hostname do not share a derived MQTT client_id. It is persisted next to the
// log under the user config dir; if that is not possible it falls back to a
// per-process value rather than failing startup.
func installSuffix() string {
	dir, err := userConfigDir()
	if err != nil {
		return processSuffix()
	}
	path := filepath.Join(dir, "callmqtt", "install_id")
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); validSuffix(s) {
			return s
		}
	}
	var raw [3]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return processSuffix()
	}
	s := hex.EncodeToString(raw[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return processSuffix()
	}
	if err := os.WriteFile(path, []byte(s+"\n"), 0o600); err != nil {
		return processSuffix()
	}
	return s
}

func processSuffix() string {
	return fmt.Sprintf("%06x", os.Getpid()&0xffffff)
}

func validSuffix(s string) bool {
	if len(s) != 6 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
