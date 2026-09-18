package network

import (
	"strings"
	"testing"

	"github.com/crs2007/callmqtt/internal/config"
)

func TestCheckCapabilities(t *testing.T) {
	localCaps := Capabilities{CIDR: true}

	tests := []struct {
		name    string
		rules   []config.NetworkRule
		caps    Capabilities
		wantErr bool
		wantMsg []string // substrings that must all appear in the error
	}{
		{
			name:  "cidrs-only rule is fine on LocalChecker",
			rules: []config.NetworkRule{{Name: "Home", CIDRs: []string{"192.168.1.0/24"}}},
			caps:  localCaps,
		},
		{
			name:  "gateways-only rule is fine when the checker supports it",
			rules: []config.NetworkRule{{Name: "Home", Gateways: []string{"192.168.1.1"}}},
			caps:  Capabilities{Gateway: true},
		},
		{
			name:    "ssids-only rule is rejected on LocalChecker",
			rules:   []config.NetworkRule{{Name: "Home", SSIDs: []string{"Sharon-Home"}}},
			caps:    localCaps,
			wantErr: true,
			wantMsg: []string{`rule "Home"`, "ssids only", "SSID detection is not implemented", "add a cidrs entry"},
		},
		{
			name:    "bssids-only rule is rejected on LocalChecker",
			rules:   []config.NetworkRule{{Name: "Office", BSSIDs: []string{"00:1A:2B:3C:4D:5E"}}},
			caps:    localCaps,
			wantErr: true,
			wantMsg: []string{`rule "Office"`, "bssids only", "BSSID detection is not implemented"},
		},
		{
			name:    "gateways-only rule is rejected when unsupported",
			rules:   []config.NetworkRule{{Name: "Home", Gateways: []string{"192.168.1.1"}}},
			caps:    localCaps,
			wantErr: true,
			wantMsg: []string{`rule "Home"`, "gateways only", "Gateway detection is not implemented"},
		},
		{
			name: "mixing a supported and an unsupported matcher passes",
			rules: []config.NetworkRule{
				{Name: "Home", SSIDs: []string{"Sharon-Home"}, CIDRs: []string{"192.168.1.0/24"}},
			},
			caps: localCaps,
		},
		{
			name: "two unsupported matchers together are still rejected, naming both",
			rules: []config.NetworkRule{
				{Name: "Home", SSIDs: []string{"Sharon-Home"}, BSSIDs: []string{"00:1A:2B:3C:4D:5E"}},
			},
			caps:    localCaps,
			wantErr: true,
			wantMsg: []string{`rule "Home"`, "ssids, bssids only", "SSID/BSSID"},
		},
		{
			name: "multiple offending rules are all reported",
			rules: []config.NetworkRule{
				{Name: "Home", SSIDs: []string{"Sharon-Home"}},
				{Name: "Office", BSSIDs: []string{"00:1A:2B:3C:4D:5E"}},
			},
			caps:    localCaps,
			wantErr: true,
			wantMsg: []string{`rule "Home"`, `rule "Office"`},
		},
		{
			name:  "a rule with no matchers at all is not this function's problem",
			rules: []config.NetworkRule{{Name: "Empty"}},
			caps:  localCaps,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckCapabilities(tt.rules, tt.caps)
			if tt.wantErr && err == nil {
				t.Fatalf("CheckCapabilities() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CheckCapabilities() = %v, want nil", err)
			}
			for _, want := range tt.wantMsg {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err.Error(), want)
				}
			}
		})
	}
}
