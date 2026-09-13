package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// routeProbe is an address used only to ask the operating system which local
// interface would carry traffic to the outside world. It is in TEST-NET-1
// (RFC 5737), so it is guaranteed not to be a real host.
const routeProbe = "192.0.2.1:9"

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

// Current reports the interface that would carry outbound traffic, along with
// its address and subnet.
func (LocalChecker) Current(context.Context) (Info, error) {
	local, err := outboundAddr()
	if err != nil {
		// No route to anywhere means no usable network. That is a legitimate
		// state, not a failure, and it denies.
		return Info{Connected: false}, nil
	}

	iface, prefix, err := interfaceFor(local)
	if err != nil {
		// The address is real even if its interface could not be identified,
		// so subnet matching can still work from the address alone.
		return Info{Connected: true, LocalIP: local}, nil
	}

	return Info{
		Connected: true,
		Interface: iface,
		LocalIP:   local,
		Prefix:    prefix,
	}, nil
}

// outboundAddr returns the source address the OS would use to reach the
// internet. Dialing UDP sends no packets; it only resolves the route.
func outboundAddr() (netip.Addr, error) {
	conn, err := net.Dial("udp4", routeProbe)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve outbound route: %w", err)
	}
	defer conn.Close()

	addrPort, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, errors.New("resolve outbound route: unexpected address type")
	}

	addr, ok := netip.AddrFromSlice(addrPort.IP)
	if !ok {
		return netip.Addr{}, errors.New("resolve outbound route: unparseable address")
	}
	return addr.Unmap(), nil
}

// interfaceFor finds the interface holding the given address, and the subnet
// it is configured with.
func interfaceFor(addr netip.Addr) (string, netip.Prefix, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", netip.Prefix{}, fmt.Errorf("enumerate interfaces: %w", err)
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue // an interface that cannot be read is not evidence of anything
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			candidate, ok := netip.AddrFromSlice(ipNet.IP)
			if !ok || candidate.Unmap() != addr {
				continue
			}

			ones, _ := ipNet.Mask.Size()
			prefix, err := addr.Prefix(ones)
			if err != nil {
				return iface.Name, netip.Prefix{}, nil
			}
			return iface.Name, prefix, nil
		}
	}

	return "", netip.Prefix{}, fmt.Errorf("no interface holds address %s", addr)
}
