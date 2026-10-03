package network

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// virtualAdapterNames is a heuristic denylist of interface-name substrings
// that mean "not a physical network path" on Windows: VPN split-tunnel
// adapters, hypervisor host-only/NAT adapters, and personal-area-network
// stacks. None of these tell you what network the machine is actually on —
// a VMware NAT adapter reports the same subnet on every machine that has
// VMware installed — so their addresses must never count as evidence for an
// allow-list match. This is a name match, not a real classification: a
// user-renamed adapter can dodge it, and a real adapter that happens to
// contain one of these words would wrongly be skipped. The proper fix is
// classifying by IfType/OperStatus via GetAdaptersAddresses on Windows
// (platform/windows's job, filed as a follow-up); this list is the cheap,
// platform-independent stopgap.
//
// On Windows, adapterAllowed (adapters_windows.go) now classifies by IfType
// and OperStatus first; this list remains a second filter, and the only one
// on other platforms.
var virtualAdapterNames = []string{
	"vEthernet",
	"VirtualBox Host-Only",
	"VMware",
	"Hyper-V",
	"WSL",
	"Loopback",
	"Bluetooth",
	// VPN / tunnel adapters: their address says where the VPN hands out
	// addresses, not where the machine physically is.
	"TAP-Windows",
	"Wintun",
	"WireGuard",
	"Tailscale",
	"ZeroTier",
	"NordLynx",
	"AnyConnect",
	"PANGP",
	"Fortinet",
	"OpenVPN",
}

func isVirtualAdapterName(name string) bool {
	for _, virtual := range virtualAdapterNames {
		if strings.Contains(strings.ToLower(name), strings.ToLower(virtual)) {
			return true
		}
	}
	return false
}

// LocalChecker reports the network using nothing but the standard library.
//
// The plan originally put this behind a platform adapter, but identifying the
// outbound interface and its subnet needs no syscalls, so there is nothing
// platform-specific left to abstract. It works identically on Windows and
// macOS.
//
// SSID and BSSID are deliberately left empty. Reading them needs native WLAN
// calls on Windows whose text output is localized, and location permission on
// modern macOS. Subnet matching covers Wi-Fi and Ethernet alike and cannot be
// broken by a system language change, so it is what v0.1 matches on.
type LocalChecker struct{}

// Current reports every usable address on an interface that is currently up.
// A VPN may own the default route, so selecting only the outbound interface
// would hide an otherwise allowed physical adapter.
func (LocalChecker) Current(context.Context) ([]Info, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate interfaces: %w", err)
	}

	return infosForInterfaces(ifaces, func(iface net.Interface) ([]net.Addr, error) {
		return iface.Addrs()
	}, adapterAllowed()), nil
}

// Capabilities reports that LocalChecker can only ever populate Prefix
// (subnet) matching — SSID, BSSID and Gateway are never set by Current, so a
// rule relying on any of those alone would never match. See
// CheckCapabilities.
func (LocalChecker) Capabilities() Capabilities {
	return Capabilities{CIDR: true}
}

// infosForInterfaces turns interfaces into candidates. allowed, when non-nil,
// is the platform's adapter-type check (physical Ethernet / Wi-Fi only).
func infosForInterfaces(ifaces []net.Interface, addrs func(net.Interface) ([]net.Addr, error), allowed func(net.Interface) bool) []Info {
	var infos []Info
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// FlagRunning means the adapter is actually carrying traffic, not
		// merely administratively enabled — a Wi-Fi adapter with no AP
		// joined is Up but not Running, and its address (if any) is not
		// evidence of being on any particular network.
		if iface.Flags&net.FlagRunning == 0 {
			continue
		}
		if isVirtualAdapterName(iface.Name) {
			continue
		}
		if allowed != nil && !allowed(iface) {
			continue
		}

		assigned, err := addrs(iface)
		if err != nil {
			continue // an interface that cannot be read is not evidence of anything
		}
		for _, assignedAddr := range assigned {
			addr, prefix, ok := usablePrefix(assignedAddr)
			if !ok {
				continue
			}
			infos = append(infos, Info{
				Connected: true,
				Interface: iface.Name,
				LocalIP:   addr,
				Prefix:    prefix,
			})
		}
	}
	return infos
}

// cgnat is the shared address space (RFC 6598) handed out by Tailscale,
// carrier NAT and similar overlays. Go reports it as global unicast, but it
// identifies no particular network.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func usablePrefix(assigned net.Addr) (netip.Addr, netip.Prefix, bool) {
	ipNet, ok := assigned.(*net.IPNet)
	if !ok {
		return netip.Addr{}, netip.Prefix{}, false
	}

	addr, ok := netip.AddrFromSlice(ipNet.IP)
	if !ok {
		return netip.Addr{}, netip.Prefix{}, false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || cgnat.Contains(addr) {
		return netip.Addr{}, netip.Prefix{}, false
	}

	ones, bits := ipNet.Mask.Size()
	if ones < 0 || bits != addr.BitLen() {
		return addr, netip.Prefix{}, true
	}
	return addr, netip.PrefixFrom(addr, ones).Masked(), true
}
