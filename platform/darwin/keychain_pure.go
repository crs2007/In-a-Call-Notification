package darwin

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrKeychainItemNotFound means the keychain holds no generic password for
// the requested service and account.
var ErrKeychainItemNotFound = errors.New("keychain item not found")

// securityTool is invoked by absolute path, like launchctl. Going through the
// security CLI instead of the Security framework needs no cgo, and an item
// created with `security add-generic-password` already trusts this binary,
// so reading it back raises no "allow access?" prompt.
const securityTool = "/usr/bin/security"

// errSecItemNotFound is security's exit status when no item matches.
const errSecItemNotFound = 44

// keychainPassword reads the generic password for service and account.
//
// It asks with -g, not -w: -w prints a password holding any non-printable
// byte as bare hex, which cannot be told apart from a password that merely
// is hex ("0a1b"). -g labels the two forms on stderr (see
// parseSecurityPassword), so every password reads back exactly.
func keychainPassword(run runFunc, service, account string) (string, error) {
	_, stderr, err := run(securityTool, "find-generic-password", "-s", service, "-a", account, "-g")
	if code, ok := exitCode(err); ok && code == errSecItemNotFound {
		return "", ErrKeychainItemNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read keychain item: %w", err)
	}
	return parseSecurityPassword(stderr)
}

// parseSecurityPassword reads the password line `security -g` writes to
// stderr. Printable ASCII without " or \ is written quoted and verbatim:
//
//	password: "s3cret"
//
// anything else as 0x-prefixed hex, then a lossy escaped preview:
//
//	password: 0x70C3A4  "p\303\244"
//
// and an empty password as a bare "password: ".
func parseSecurityPassword(stderr []byte) (string, error) {
	for _, line := range strings.Split(string(stderr), "\n") {
		rest, ok := strings.CutPrefix(line, "password:")
		if !ok {
			continue
		}
		rest = strings.TrimPrefix(rest, " ")
		switch {
		case rest == "":
			return "", nil
		case strings.HasPrefix(rest, "0x"):
			digits, _, _ := strings.Cut(rest[2:], " ")
			decoded, err := hex.DecodeString(digits)
			if err != nil {
				return "", fmt.Errorf("decode keychain password: %w", err)
			}
			return string(decoded), nil
		case len(rest) >= 2 && rest[0] == '"' && rest[len(rest)-1] == '"':
			return rest[1 : len(rest)-1], nil
		default:
			return "", errors.New("unrecognised password line from security")
		}
	}
	return "", errors.New("security printed no password")
}
