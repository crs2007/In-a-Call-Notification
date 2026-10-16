//go:build darwin

package main

import (
	"errors"

	"github.com/crs2007/callmqtt/internal/config"
	platformdarwin "github.com/crs2007/callmqtt/platform/darwin"
)

// init wires the keychain into config before main can load anything, so a
// `password: "keychain:callmqtt"` resolves on the first load and on every
// reload the tray triggers alike.
func init() { config.KeychainLookup = keychainLookup }

// readKeychain is platformdarwin.KeychainPassword, swappable in tests so
// they never touch the real keychain.
var readKeychain = platformdarwin.KeychainPassword

// keychainLookup translates platform/darwin's not-found error into the one
// config recognises, which turns it into the "create it with security
// add-generic-password ..." advice.
func keychainLookup(service, account string) (string, error) {
	secret, err := readKeychain(service, account)
	if errors.Is(err, platformdarwin.ErrKeychainItemNotFound) {
		return "", config.ErrKeychainItemNotFound
	}
	return secret, err
}
