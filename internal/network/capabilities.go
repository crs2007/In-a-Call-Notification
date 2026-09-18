package network

import (
	"errors"
	"fmt"
	"strings"

	"github.com/crs2007/callmqtt/internal/config"
)

// Capabilities reports which of Info's matchable fields a Checker
// implementation can actually populate. It exists because a rule that only
// matches on a field the running Checker never fills in (SSID on a platform
// with no WLAN API wired up, say) would otherwise compile without error and
// then silently never match — the bulb just never turns on and nobody knows
// why. See CheckCapabilities.
type Capabilities struct {
	CIDR    bool
	SSID    bool
	BSSID   bool
	Gateway bool
}

// supportedFields lists, in preference order, the config field name for each
// capability this value supports. Used to phrase a "did you mean" suggestion
// in CheckCapabilities.
func (c Capabilities) supportedFields() []string {
	var names []string
	if c.CIDR {
		names = append(names, "cidrs")
	}
	if c.SSID {
		names = append(names, "ssids")
	}
	if c.BSSID {
		names = append(names, "bssids")
	}
	if c.Gateway {
		names = append(names, "gateways")
	}
	return names
}

// matchField pairs one NetworkRule field with the capability that must be
// true for it to ever match.
type matchField struct {
	name      string // config field name, e.g. "ssids"
	human     string // capability name, e.g. "SSID"
	present   bool
	supported bool
}

// CheckCapabilities rejects any rule whose only non-empty matcher fields are
// ones the given Capabilities can't populate — such a rule is accepted by
// config parsing and by NewMatcher, but can never actually match anything on
// this platform. A rule that mixes a supported field with an unsupported one
// (ssids + cidrs, say) is fine: the supported field still works.
//
// Every offending rule is reported, not just the first — same convention as
// config.Config.Validate.
func CheckCapabilities(rules []config.NetworkRule, caps Capabilities) error {
	suggestion := "no matcher field is supported on this platform; every allowed_networks rule will always be denied"
	if supported := caps.supportedFields(); len(supported) > 0 {
		suggestion = fmt.Sprintf("add a %s entry", supported[0])
	}

	var problems []error
	for _, r := range rules {
		fields := []matchField{
			{name: "ssids", human: "SSID", present: len(r.SSIDs) > 0, supported: caps.SSID},
			{name: "bssids", human: "BSSID", present: len(r.BSSIDs) > 0, supported: caps.BSSID},
			{name: "cidrs", human: "CIDR", present: len(r.CIDRs) > 0, supported: caps.CIDR},
			{name: "gateways", human: "Gateway", present: len(r.Gateways) > 0, supported: caps.Gateway},
		}

		var presentNames, unsupportedHuman []string
		anySupported := false
		for _, f := range fields {
			if !f.present {
				continue
			}
			presentNames = append(presentNames, f.name)
			if f.supported {
				anySupported = true
			} else {
				unsupportedHuman = append(unsupportedHuman, f.human)
			}
		}

		if len(presentNames) == 0 || anySupported {
			continue // no matchers at all is config.Validate's problem, not this one
		}

		problems = append(problems, fmt.Errorf(
			"rule %q matches on %s only; %s detection is not implemented on this platform, %s",
			r.Name, strings.Join(presentNames, ", "), strings.Join(unsupportedHuman, "/"), suggestion,
		))
	}

	return errors.Join(problems...)
}
