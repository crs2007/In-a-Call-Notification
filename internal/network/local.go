package network

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

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
	}), nil
}

func infosForInterfaces(ifaces []net.Interface, addrs func(net.Interface) ([]net.Addr, error)) []Info {
	var infos []Info
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
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
	if !addr.IsGlobalUnicast() {
		return netip.Addr{}, netip.Prefix{}, false
	}

	ones, bits := ipNet.Mask.Size()
	if ones < 0 || bits != addr.BitLen() {
		return addr, netip.Prefix{}, true
	}
	return addr, netip.PrefixFrom(addr, ones).Masked(), true
}
