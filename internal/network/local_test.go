package network

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
)

// The local checker talks to the real host, so this asserts only what must be
// true everywhere: it never returns an error, and anything it does report is
// self-consistent. A machine with no network is a valid outcome, not a failure.
func TestLocalChecker(t *testing.T) {
	infos, err := LocalChecker{}.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}

	for _, info := range infos {
		if !info.Connected {
			t.Errorf("reported disconnected candidate: %+v", info)
		}
		if !info.LocalIP.IsValid() {
			t.Errorf("reported candidate without an address: %+v", info)
		}
		if info.Prefix.IsValid() && !info.Prefix.Contains(info.LocalIP) {
			t.Errorf("reported address %s is outside its own subnet %s", info.LocalIP, info.Prefix)
		}
		t.Logf("interface=%q address=%s subnet=%s", info.Interface, info.LocalIP, info.Prefix)
	}
}

func TestInfosForInterfaces(t *testing.T) {
	interfaces := []net.Interface{
		{Index: 1, Name: "CatoNetworks", Flags: net.FlagUp},
		{Index: 2, Name: "Ethernet", Flags: net.FlagUp},
		{Index: 3, Name: "Docker", Flags: net.FlagUp},
		{Index: 4, Name: "Disconnected"},
		{Index: 5, Name: "Loopback", Flags: net.FlagUp | net.FlagLoopback},
	}
	addresses := map[int][]net.Addr{
		1: {ipNet("192.168.16.122", 32)},
		2: {ipNet("192.168.1.42", 24)},
		3: {ipNet("172.18.0.1", 16)},
		4: {ipNet("10.20.1.10", 16)},
		5: {ipNet("127.0.0.1", 8)},
	}

	tests := []struct {
		name string
		read func(net.Interface) ([]net.Addr, error)
		want []Info
	}{
		{
			name: "all usable addresses are reported despite VPN default route",
			read: func(iface net.Interface) ([]net.Addr, error) { return addresses[iface.Index], nil },
			want: []Info{
				{Connected: true, Interface: "CatoNetworks", LocalIP: netip.MustParseAddr("192.168.16.122"), Prefix: netip.MustParsePrefix("192.168.16.122/32")},
				{Connected: true, Interface: "Ethernet", LocalIP: netip.MustParseAddr("192.168.1.42"), Prefix: netip.MustParsePrefix("192.168.1.0/24")},
				{Connected: true, Interface: "Docker", LocalIP: netip.MustParseAddr("172.18.0.1"), Prefix: netip.MustParsePrefix("172.18.0.0/16")},
			},
		},
		{
			name: "unreadable addresses provide no evidence",
			read: func(net.Interface) ([]net.Addr, error) { return nil, errors.New("unreadable") },
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := infosForInterfaces(interfaces, tt.read); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("infos = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func ipNet(ip string, bits int) *net.IPNet {
	parsed := net.ParseIP(ip)
	if v4 := parsed.To4(); v4 != nil {
		parsed = v4
	}
	return &net.IPNet{IP: parsed, Mask: net.CIDRMask(bits, 8*len(parsed))}
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
