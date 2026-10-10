//go:build darwin

package darwin

// KeychainPassword returns the generic password stored in the user's
// keychain under service and account — the item
// `security add-generic-password -s <service> -a <account> -w` creates. It
// returns ErrKeychainItemNotFound when there is none.
//
// This exists because an app started from Finder or at login by launchd does
// not inherit a shell profile's environment variables, so the ${VAR}
// password that works on Windows reaches it empty.
func KeychainPassword(service, account string) (string, error) {
	return keychainPassword(runCommand, service, account)
}
