package network

import (
	"net/netip"
	"testing"

	"github.com/crs2007/callmqtt/internal/config"
)

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("bad test address %q: %v", s, err)
	}
	return a
}

// homeAndOffice is the shape of allow-list a real user writes: one rule per
// site, each covering both Wi-Fi and Ethernet.
func homeAndOffice() []config.NetworkRule {
	return []config.NetworkRule{
		{
			Name:     "Home",
			SSIDs:    []string{"Sharon-Home"},
			CIDRs:    []string{"192.168.1.0/24"},
			Gateways: []string{"192.168.1.1"},
		},
		{
			Name:   "Office",
			SSIDs:  []string{"Company-WiFi"},
			BSSIDs: []string{"00:1A:2B:3C:4D:5E"},
			CIDRs:  []string{"10.20.0.0/16"},
		},
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name      string
		info      Info
		wantRule  string
		wantAllow bool
	}{
		{
			name:      "home wifi by ssid",
			info:      Info{Connected: true, SSID: "Sharon-Home"},
			wantRule:  "Home",
			wantAllow: true,
		},
		{
			name:      "ssid match is case insensitive",
			info:      Info{Connected: true, SSID: "SHARON-home"},
			wantRule:  "Home",
			wantAllow: true,
		},
		{
			name:      "home ethernet by subnet, no ssid at all",
			info:      Info{Connected: true, LocalIP: addr(t, "192.168.1.42")},
			wantRule:  "Home",
			wantAllow: true,
		},
		{
			name:      "home by gateway when the ip is outside the listed subnet",
			info:      Info{Connected: true, LocalIP: addr(t, "172.16.5.5"), Gateway: addr(t, "192.168.1.1")},
			wantRule:  "Home",
			wantAllow: true,
		},
		{
			name:      "office by bssid written with different separators",
			info:      Info{Connected: true, BSSID: "00-1a-2b-3c-4d-5e"},
			wantRule:  "Office",
			wantAllow: true,
		},
		{
			name:      "office by wide subnet",
			info:      Info{Connected: true, LocalIP: addr(t, "10.20.99.7")},
			wantRule:  "Office",
			wantAllow: true,
		},
		{
			name:      "coffee shop wifi is denied",
			info:      Info{Connected: true, SSID: "Starbucks", LocalIP: addr(t, "172.20.10.4")},
			wantAllow: false,
		},
		{
			name:      "mobile hotspot is denied",
			info:      Info{Connected: true, SSID: "Sharon iPhone", LocalIP: addr(t, "172.20.10.2")},
			wantAllow: false,
		},
		{
			name:      "address just outside the home subnet is denied",
			info:      Info{Connected: true, LocalIP: addr(t, "192.168.2.1")},
			wantAllow: false,
		},
		{
			name:      "disconnected is denied even with a matching cached ssid",
			info:      Info{Connected: false, SSID: "Sharon-Home", LocalIP: addr(t, "192.168.1.42")},
			wantAllow: false,
		},
		{
			name:      "empty info is denied",
			info:      Info{},
			wantAllow: false,
		},
	}

	m, err := NewMatcher(homeAndOffice())
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRule, gotAllow := m.Match(tt.info)
			if gotAllow != tt.wantAllow {
				t.Fatalf("allowed = %v, want %v", gotAllow, tt.wantAllow)
			}
			if gotRule != tt.wantRule {
				t.Errorf("rule = %q, want %q", gotRule, tt.wantRule)
			}
		})
	}
}

func TestMatchAny(t *testing.T) {
	vpn := Info{Connected: true, Interface: "CatoNetworks", LocalIP: addr(t, "192.168.16.122")}
	home := Info{Connected: true, Interface: "Ethernet", LocalIP: addr(t, "192.168.1.42")}
	docker := Info{Connected: true, Interface: "Docker", LocalIP: addr(t, "172.18.0.1")}

	tests := []struct {
		name      string
		infos     []Info
		wantInfo  Info
		wantRule  string
		wantAllow bool
	}{
		{
			name:      "VPN candidate does not hide allowed physical adapter",
			infos:     []Info{vpn, home},
			wantInfo:  home,
			wantRule:  "Home",
			wantAllow: true,
		},
		{
			name:  "no matching adapter denies",
			infos: []Info{vpn, docker},
		},
		{
			name:  "disconnected matching adapter denies",
			infos: []Info{{Connected: false, Interface: "Ethernet", LocalIP: addr(t, "192.168.1.42")}},
		},
		{
			name: "empty candidates deny",
		},
	}

	m, err := NewMatcher(homeAndOffice())
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInfo, gotRule, gotAllow := m.MatchAny(tt.infos)
			if gotAllow != tt.wantAllow || gotRule != tt.wantRule || gotInfo != tt.wantInfo {
				t.Fatalf("MatchAny() = (%+v, %q, %v), want (%+v, %q, %v)",
					gotInfo, gotRule, gotAllow, tt.wantInfo, tt.wantRule, tt.wantAllow)
			}
		})
	}
}

func TestVirtualAdapterRequiresExplicitAddressMatch(t *testing.T) {
	docker := Info{Connected: true, Interface: "Docker", LocalIP: addr(t, "172.18.0.1")}
	tests := []struct {
		name      string
		rules     []config.NetworkRule
		wantAllow bool
	}{
		{
			name:  "private virtual address alone denies",
			rules: homeAndOffice(),
		},
		{
			name:      "explicit virtual address CIDR allows",
			rules:     []config.NetworkRule{{Name: "Lab", CIDRs: []string{"172.18.0.0/16"}}},
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMatcher(tt.rules)
			if err != nil {
				t.Fatalf("NewMatcher: %v", err)
			}
			_, _, gotAllow := m.MatchAny([]Info{docker})
			if gotAllow != tt.wantAllow {
				t.Fatalf("allowed = %v, want %v", gotAllow, tt.wantAllow)
			}
		})
	}
}

// An empty allow-list must publish nothing. This is the single most important
// assertion in the package: it is the difference between a misconfigured agent
// staying quiet and one broadcasting presence from anywhere.
func TestEmptyAllowListDeniesEverything(t *testing.T) {
	m, err := NewMatcher(nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	info := Info{Connected: true, SSID: "Sharon-Home", LocalIP: addr(t, "192.168.1.42")}
	if _, allowed := m.Match(info); allowed {
		t.Error("an empty allow-list must deny, but it allowed")
	}
}

func TestIPv6(t *testing.T) {
	m, err := NewMatcher([]config.NetworkRule{
		{Name: "Home v6", CIDRs: []string{"2001:db8::/32"}},
	})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	if _, allowed := m.Match(Info{Connected: true, LocalIP: addr(t, "2001:db8::1")}); !allowed {
		t.Error("address inside the v6 prefix should be allowed")
	}
	if _, allowed := m.Match(Info{Connected: true, LocalIP: addr(t, "2001:dead::1")}); allowed {
		t.Error("address outside the v6 prefix should be denied")
	}
	// A v4 address must not accidentally satisfy a v6 prefix.
	if _, allowed := m.Match(Info{Connected: true, LocalIP: addr(t, "192.168.1.42")}); allowed {
		t.Error("v4 address should not match a v6 prefix")
	}
}

// A v4-mapped v6 address (::ffff:192.168.1.42) is the same host as
// 192.168.1.42 and must match a v4 rule.
func TestIPv4MappedAddressMatchesV4Prefix(t *testing.T) {
	m, err := NewMatcher([]config.NetworkRule{
		{Name: "Home", CIDRs: []string{"192.168.1.0/24"}},
	})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if _, allowed := m.Match(Info{Connected: true, LocalIP: addr(t, "::ffff:192.168.1.42")}); !allowed {
		t.Error("v4-mapped address should match the equivalent v4 prefix")
	}
}

// A CIDR with host bits set, such as 192.168.1.42/24, is what users actually
// type. It must behave as the 192.168.1.0/24 network.
func TestUnmaskedCIDRIsNormalized(t *testing.T) {
	m, err := NewMatcher([]config.NetworkRule{
		{Name: "Home", CIDRs: []string{"192.168.1.42/24"}},
	})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if _, allowed := m.Match(Info{Connected: true, LocalIP: addr(t, "192.168.1.7")}); !allowed {
		t.Error("a CIDR with host bits set should still match its whole subnet")
	}
}

func TestNewMatcherRejectsMalformedRules(t *testing.T) {
	tests := []struct {
		name string
		rule config.NetworkRule
	}{
		{"bad cidr", config.NetworkRule{Name: "X", CIDRs: []string{"192.168.1.0/33"}}},
		{"cidr without prefix length", config.NetworkRule{Name: "X", CIDRs: []string{"192.168.1.0"}}},
		{"bad gateway", config.NetworkRule{Name: "X", Gateways: []string{"192.168.1.256"}}},
		{"gateway is a hostname", config.NetworkRule{Name: "X", Gateways: []string{"router.local"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewMatcher([]config.NetworkRule{tt.rule}); err == nil {
				t.Error("expected a malformed rule to be rejected at construction")
			}
		})
	}
}

func TestNormalizeBSSID(t *testing.T) {
	want := "001a2b3c4d5e"
	for _, in := range []string{"00:1A:2B:3C:4D:5E", "00-1a-2b-3c-4d-5e", "001A2B3C4D5E", "00 1a 2b 3c 4d 5e"} {
		if got := normalizeBSSID(in); got != want {
			t.Errorf("normalizeBSSID(%q) = %q, want %q", in, got, want)
		}
	}
}
