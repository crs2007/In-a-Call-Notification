package config

import (
	"errors"
	"fmt"
	"strings"
)

// keychainPrefix marks mqtt.password as a reference to a macOS keychain item
// rather than the secret itself: "keychain:callmqtt" means the generic
// password with service "callmqtt" and account mqtt.username.
//
// It exists because an app started from Finder or at login by launchd never
// sees a shell profile's environment, so the ${CALLMQTT_MQTT_PASSWORD} that
// works on Windows arrives empty on macOS.
const keychainPrefix = "keychain:"

// KeychainLookup returns the password stored under service and account, and
// an error wrapping ErrKeychainItemNotFound when there is no such item.
//
// config cannot import platform code, so cmd/callmqtt sets this on macOS
// before loading anything. Left nil (every other OS), a keychain: reference
// is a validation error. Load calls it, so the agent's first load and every
// reload after a tray change resolve the secret the same way; Parse only
// checks the reference, so Save's validation never touches the keychain.
var KeychainLookup func(service, account string) (string, error)

// ErrKeychainItemNotFound is what a KeychainLookup wraps when the keychain
// has no item for the service and account.
var ErrKeychainItemNotFound = errors.New("keychain item not found")

// parseKeychainRef records a keychain: reference in the raw password. It
// runs before ${VAR} expansion, so only a reference written in the file
// counts — not one smuggled in through an environment variable.
func (c *Config) parseKeychainRef() {
	service, ok := strings.CutPrefix(c.MQTT.Password, keychainPrefix)
	c.passwordFromKeychain = ok
	c.keychainService = strings.TrimSpace(service)
}

// validateKeychainRef reports what stops a keychain: reference from being
// resolved, all at once, in Validate's style.
func (c *Config) validateKeychainRef(add func(format string, args ...any)) {
	if !c.passwordFromKeychain {
		return
	}
	if KeychainLookup == nil {
		add("mqtt.password: keychain: references are only supported on macOS")
		return
	}
	if c.keychainService == "" {
		add("mqtt.password %q names no keychain service; write it as keychain:<service>, e.g. keychain:callmqtt", c.MQTT.Password)
	}
	if c.MQTT.Username == "" {
		add("mqtt.username is required with a keychain: password, since it is the keychain item's account")
	}
}

// resolveKeychain replaces a keychain: reference with the secret it names.
func (c *Config) resolveKeychain() error {
	if !c.passwordFromKeychain {
		return nil
	}
	service, account := c.keychainService, c.MQTT.Username
	secret, err := KeychainLookup(service, account)
	if errors.Is(err, ErrKeychainItemNotFound) {
		return fmt.Errorf("no keychain item for service %s account %s: create it with `security add-generic-password -U -s %s -a %s -w`",
			service, account, shellQuote(service), shellQuote(account))
	}
	if err != nil {
		return fmt.Errorf("read mqtt.password from keychain service %s account %s: %w", service, account, err)
	}
	if secret == "" {
		return fmt.Errorf("keychain item for service %s account %s holds an empty password: set it with `security add-generic-password -U -s %s -a %s -w`",
			service, account, shellQuote(service), shellQuote(account))
	}
	c.MQTT.Password = secret
	return nil
}

// shellQuote quotes s for a copy-pasted shell command, leaving the common
// case (letters, digits, @._+-) bare so the hint stays readable.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@._+-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
