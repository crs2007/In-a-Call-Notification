//go:build darwin

package network

import (
	"net"
	"testing"
)

// TestAdapterAllowedOnThisMac checks the wired-in predicate against the real
// interface table: it must be installed (non-nil), loopback must never pass,
// and nothing that passes may be a known virtual family.
func TestAdapterAllowedOnThisMac(t *testing.T) {
	allowed := adapterAllowed()
	if allowed == nil {
		t.Fatal("adapterAllowed() = nil on darwin; want the macOS classifier")
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces: %v", err)
	}
	for _, iface := range ifaces {
		ok := allowed(iface)
		t.Logf("%-10s flags=%-50v allowed=%v", iface.Name, iface.Flags, ok)
		if ok && (iface.Flags&net.FlagLoopback != 0 || isMacVirtualInterfaceName(iface.Name)) {
			t.Errorf("adapterAllowed(%s) = true for a loopback/virtual interface", iface.Name)
		}
	}
}
