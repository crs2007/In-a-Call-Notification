package network

import (
	"net"
	"testing"
)

// physicalFlags are the flags macOS sets on a connected en<N> interface.
const physicalFlags = net.FlagUp | net.FlagBroadcast | net.FlagMulticast | net.FlagRunning

func TestMacAdapterAllowed(t *testing.T) {
	cases := []struct {
		name  string
		iface net.Interface
		want  bool
	}{
		{"built-in Wi-Fi", net.Interface{Name: "en0", Flags: physicalFlags}, true},
		{"USB/Thunderbolt Ethernet", net.Interface{Name: "en7", Flags: physicalFlags}, true},

		// VPN tunnels: point-to-point and not broadcast, and named utun.
		{"WireGuard/Tailscale VPN", net.Interface{Name: "utun3", Flags: net.FlagUp | net.FlagPointToPoint | net.FlagMulticast | net.FlagRunning}, false},
		// Even if a tunnel somehow advertised broadcast, the name rejects it.
		{"utun with broadcast flags", net.Interface{Name: "utun3", Flags: physicalFlags}, false},
		{"IPsec VPN", net.Interface{Name: "ipsec0", Flags: physicalFlags}, false},
		{"PPP", net.Interface{Name: "ppp0", Flags: net.FlagUp | net.FlagPointToPoint}, false},

		// Virtual interfaces that look exactly like Ethernet by flags.
		{"VM / Internet Sharing bridge", net.Interface{Name: "bridge100", Flags: physicalFlags}, false},
		{"Thunderbolt Bridge", net.Interface{Name: "bridge0", Flags: physicalFlags}, false},
		{"AWDL", net.Interface{Name: "awdl0", Flags: physicalFlags}, false},
		{"low-latency WLAN", net.Interface{Name: "llw0", Flags: physicalFlags}, false},
		{"Apple network processor", net.Interface{Name: "anpi0", Flags: physicalFlags}, false},
		{"access point", net.Interface{Name: "ap1", Flags: physicalFlags}, false},
		{"Virtualization.framework", net.Interface{Name: "vmenet0", Flags: physicalFlags}, false},
		{"VMware", net.Interface{Name: "vmnet8", Flags: physicalFlags}, false},
		{"Parallels", net.Interface{Name: "vnic0", Flags: physicalFlags}, false},
		{"gif tunnel", net.Interface{Name: "gif0", Flags: net.FlagUp | net.FlagPointToPoint | net.FlagMulticast}, false},
		{"6to4 tunnel", net.Interface{Name: "stf0", Flags: net.FlagUp}, false},

		{"loopback", net.Interface{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback | net.FlagMulticast | net.FlagRunning}, false},
		{"down Ethernet", net.Interface{Name: "en1", Flags: net.FlagBroadcast | net.FlagMulticast}, false},
		{"en without broadcast", net.Interface{Name: "en5", Flags: net.FlagUp | net.FlagRunning}, false},
		{"point-to-point en", net.Interface{Name: "en9", Flags: physicalFlags | net.FlagPointToPoint}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := macAdapterAllowed(c.iface); got != c.want {
				t.Errorf("macAdapterAllowed(%s, %v) = %v, want %v", c.iface.Name, c.iface.Flags, got, c.want)
			}
		})
	}
}

func TestIsMacVirtualInterfaceName(t *testing.T) {
	cases := map[string]bool{
		"utun3":     true,
		"bridge100": true,
		"awdl0":     true,
		"llw0":      true,
		"anpi0":     true,
		"ap1":       true,
		"lo0":       true,
		"UTUN0":     true,
		"en0":       false,
		"en7":       false,
		"":          false,
	}
	for name, want := range cases {
		if got := isMacVirtualInterfaceName(name); got != want {
			t.Errorf("isMacVirtualInterfaceName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestMacAdapterAllowedThroughInfos runs the predicate through the same
// infosForInterfaces path LocalChecker uses, so a VPN holding a
// private-looking address can't contribute a candidate network.
func TestMacAdapterAllowedThroughInfos(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "en0", Flags: physicalFlags},
		{Index: 2, Name: "utun3", Flags: net.FlagUp | net.FlagPointToPoint | net.FlagRunning},
		{Index: 3, Name: "bridge100", Flags: physicalFlags},
	}
	addrs := map[string]string{
		"en0":       "192.168.1.20/24",
		"utun3":     "10.8.0.2/24",
		"bridge100": "192.168.64.1/24",
	}
	infos := infosForInterfaces(ifaces, func(iface net.Interface) ([]net.Addr, error) {
		ip, ipNet, err := net.ParseCIDR(addrs[iface.Name])
		if err != nil {
			return nil, err
		}
		ipNet.IP = ip
		return []net.Addr{ipNet}, nil
	}, macAdapterAllowed)

	if len(infos) != 1 || infos[0].Interface != "en0" {
		t.Fatalf("infosForInterfaces() = %+v, want only en0", infos)
	}
	if got := infos[0].Prefix.String(); got != "192.168.1.0/24" {
		t.Errorf("en0 prefix = %s, want 192.168.1.0/24", got)
	}
}
