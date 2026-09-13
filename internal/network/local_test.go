package network

import (
	"context"
	"testing"
)

// The local checker talks to the real host, so this asserts only what must be
// true everywhere: it never returns an error, and anything it does report is
// self-consistent. A machine with no network is a valid outcome, not a failure.
func TestLocalChecker(t *testing.T) {
	info, err := LocalChecker{}.Current(context.Background())
	if err != nil {
		t.Fatalf("the local checker must degrade rather than fail: %v", err)
	}

	if !info.Connected {
		t.Skip("no outbound route on this machine")
	}
	if !info.LocalIP.IsValid() {
		t.Error("connected but reported no address")
	}
	if info.Prefix.IsValid() && !info.Prefix.Contains(info.LocalIP) {
		t.Errorf("reported address %s is outside its own subnet %s", info.LocalIP, info.Prefix)
	}

	t.Logf("interface=%q address=%s subnet=%s", info.Interface, info.LocalIP, info.Prefix)
}

// A failure to read the network must deny rather than allow.
func TestUnreadableNetworkDenies(t *testing.T) {
	m, err := NewMatcher(nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if _, allowed := m.Match(Info{Connected: false}); allowed {
		t.Error("a disconnected machine must be denied")
	}
}
