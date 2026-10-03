//go:build windows

package network

import (
	"log/slog"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ifTypeEthernet = 6  // IF_TYPE_ETHERNET_CSMACD
	ifTypeWiFi     = 71 // IF_TYPE_IEEE80211
)

// physicalAdapterIndexes returns the interface indexes (IPv4 and IPv6) of
// adapters that are operationally up and are real Ethernet or Wi-Fi. Tunnel,
// PPP, loopback and every other IfType are excluded: their addresses do not
// say which physical network the machine is on.
func physicalAdapterIndexes() (map[uint32]bool, error) {
	const flags = windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER
	size := uint32(15 * 1024)
	buf := make([]byte, size)
	for {
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, err
		}
		buf = make([]byte, size)
	}

	allowed := make(map[uint32]bool)
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp {
			continue
		}
		if a.IfType != ifTypeEthernet && a.IfType != ifTypeWiFi {
			continue
		}
		if a.IfIndex != 0 {
			allowed[a.IfIndex] = true
		}
		if a.Ipv6IfIndex != 0 {
			allowed[a.Ipv6IfIndex] = true
		}
	}
	return allowed, nil
}

// adapterAllowed snapshots the adapter table and returns a predicate over
// net.Interface. If the table cannot be read it denies everything: no
// evidence is safer than unclassified evidence.
func adapterAllowed() func(net.Interface) bool {
	allowed, err := physicalAdapterIndexes()
	if err != nil {
		slog.Debug("network: cannot classify adapters, treating all as unusable", "error", err)
		return func(net.Interface) bool { return false }
	}
	return func(iface net.Interface) bool { return allowed[uint32(iface.Index)] }
}
