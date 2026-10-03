// Package network decides whether the machine is currently on a network where
// publishing presence is permitted.
//
// This is the privacy boundary of the whole project: a bug that wrongly
// answers "allowed" publishes the user's call state from a coffee shop. Every
// decision here therefore fails closed — anything unrecognised, unparseable or
// unknown is denied.
package network

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/crs2007/callmqtt/internal/config"
)

// Info describes the network the machine is attached to right now.
//
// SSID and BSSID are best-effort: they are empty on Ethernet, and on some
// platforms they are unavailable without extra permissions. Matching therefore
// never depends on them alone.
type Info struct {
	Connected bool
	Interface string
	SSID      string
	BSSID     string
	LocalIP   netip.Addr
	Prefix    netip.Prefix // the subnet the interface is configured with
	Gateway   netip.Addr
}

// Checker reports the current network. Implementations live in platform/.
type Checker interface {
	Current(ctx context.Context) ([]Info, error)

	// Capabilities reports which Info fields this Checker can actually
	// populate. A rule that only matches on a field the checker never fills
	// in would silently never match, so callers use this to reject such
	// rules up front instead. See CheckCapabilities.
	Capabilities() Capabilities
}

// Matcher evaluates Info against the user's allow-list.
type Matcher struct {
	rules []rule
}

// rule is a NetworkRule with its addresses parsed once at construction.
type rule struct {
	name     string
	ssids    []string // lowercased
	bssids   []string // lowercased, separators stripped
	prefixes []netip.Prefix
	wide     []bool // parallel to prefixes: config.ClassifyCIDR said CIDRWide
	gateways []netip.Addr
}

// Match is the outcome of a successful MatchAny.
type Match struct {
	Info Info   // the candidate that matched
	Rule string // the allow-list rule's name

	// Wide reports that the rule matched only through a CIDR config flags
	// as very wide (config.CIDRWide), so the "allowed" verdict says little
	// about where the machine actually is. The tray surfaces it.
	Wide bool
}

// NewMatcher compiles the allow-list. Malformed entries are rejected here
// rather than being silently skipped at match time, where a typo in a CIDR
// would quietly widen or narrow what gets published.
func NewMatcher(rules []config.NetworkRule) (*Matcher, error) {
	compiled := make([]rule, 0, len(rules))

	for _, r := range rules {
		c := rule{name: r.Name}

		for _, ssid := range r.SSIDs {
			c.ssids = append(c.ssids, strings.ToLower(ssid))
		}
		for _, bssid := range r.BSSIDs {
			c.bssids = append(c.bssids, normalizeBSSID(bssid))
		}
		for _, cidr := range r.CIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return nil, fmt.Errorf("allowed network %q: invalid cidr %q: %w", r.Name, cidr, err)
			}
			c.prefixes = append(c.prefixes, prefix.Masked())
			c.wide = append(c.wide, config.ClassifyCIDR(prefix) == config.CIDRWide)
		}
		for _, gw := range r.Gateways {
			addr, err := netip.ParseAddr(gw)
			if err != nil {
				return nil, fmt.Errorf("allowed network %q: invalid gateway %q: %w", r.Name, gw, err)
			}
			c.gateways = append(c.gateways, addr.Unmap())
		}

		compiled = append(compiled, c)
	}

	return &Matcher{rules: compiled}, nil
}

// Match reports the name of the first allow-list rule that the network
// satisfies. The fields within a rule are alternatives: matching the SSID, the
// BSSID, the subnet or the gateway is each sufficient. That is what lets one
// rule cover the same home network over both Wi-Fi and Ethernet.
//
// An empty allow-list, a disconnected machine, or a network matching nothing
// all deny.
func (m *Matcher) Match(info Info) (string, bool) {
	match, allowed := m.MatchAny([]Info{info})
	return match.Rule, allowed
}

// MatchAny reports the first allow-list rule satisfied by any connected
// candidate, along with the candidate that matched. Rule order remains the
// policy tie-breaker; interface names and address ranges are never treated as
// evidence unless an existing rule explicitly matches them.
func (m *Matcher) MatchAny(infos []Info) (Match, bool) {
	for _, r := range m.rules {
		for _, info := range infos {
			if !info.Connected {
				continue
			}
			if ok, wide := r.matches(info); ok {
				return Match{Info: info, Rule: r.name, Wide: wide}, true
			}
		}
	}
	return Match{}, false
}

// matches reports whether info satisfies the rule and, if it does, whether
// the only evidence was a wide CIDR. Any narrower evidence — an SSID, BSSID,
// gateway or narrow CIDR — wins over a wide CIDR in the same rule.
func (r rule) matches(info Info) (ok, wide bool) {
	if info.SSID != "" {
		want := strings.ToLower(info.SSID)
		for _, ssid := range r.ssids {
			if ssid == want {
				return true, false
			}
		}
	}

	if info.BSSID != "" {
		want := normalizeBSSID(info.BSSID)
		for _, bssid := range r.bssids {
			if bssid == want {
				return true, false
			}
		}
	}

	wideHit := false
	if ip := info.LocalIP.Unmap(); ip.IsValid() {
		for i, prefix := range r.prefixes {
			if !prefix.Contains(ip) {
				continue
			}
			if !r.wide[i] {
				return true, false
			}
			wideHit = true
		}
	}

	if gw := info.Gateway.Unmap(); gw.IsValid() {
		for _, want := range r.gateways {
			if want == gw {
				return true, false
			}
		}
	}

	return wideHit, wideHit
}

// normalizeBSSID reduces a MAC address to lowercase hex digits so that
// "00:1A:2B:3C:4D:5E", "00-1a-2b-3c-4d-5e" and "001a2b3c4d5e" compare equal.
func normalizeBSSID(bssid string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(bssid) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
