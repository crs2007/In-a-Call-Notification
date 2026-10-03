//go:build !windows

package network

import "net"

// adapterAllowed has no platform classification off Windows; the name
// denylist in local.go is the only adapter filter there.
func adapterAllowed() func(net.Interface) bool { return nil }
