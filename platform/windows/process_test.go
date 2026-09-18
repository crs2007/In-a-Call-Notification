//go:build windows

package windows

import (
	"os"
	"testing"
)

// TestProcessNamesIncludesSelf is a basic sanity check on the Toolhelp32
// snapshot walk: the test binary's own PID must show up with a non-empty
// executable name.
func TestProcessNamesIncludesSelf(t *testing.T) {
	names := ProcessNames()

	pid := uint32(os.Getpid())
	name, ok := names[pid]
	if !ok {
		t.Fatalf("ProcessNames() missing current PID %d", pid)
	}
	if name == "" {
		t.Fatalf("ProcessNames()[%d] is empty", pid)
	}
}
