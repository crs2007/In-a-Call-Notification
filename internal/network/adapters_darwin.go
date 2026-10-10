//go:build darwin

package network

import "net"

// adapterAllowed classifies macOS interfaces by flags and BSD name (see
// macAdapterAllowed), so only physical Ethernet and Wi-Fi addresses count
// toward an allowed_networks match. The name denylist in local.go still
// applies on top, though its Windows-oriented entries rarely matter here.
//
// Unlike Windows' GetAdaptersAddresses walk, this needs no system call
// beyond the net.Interfaces() the caller already made, so it cannot fail and
// never has to fall back to denying everything.
func adapterAllowed() func(net.Interface) bool { return macAdapterAllowed }
