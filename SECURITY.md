# Security Policy

CallMQTT is a single-binary desktop agent; there is no supported-versions
matrix to publish here — the latest release is the only one that gets fixes.

## Threat model & known limitations

**The network gate is weaker than its config schema implies.** `allowed_networks`
rules can declare `ssids`, `bssids`, `cidrs` or `gateways`, and the docs have at
times described these as interchangeable ("any one field matching is enough").
That is only true of the schema, not the running agent:

- The only implementation that actually determines the current network
  (`internal/network/local.go`'s `LocalChecker`, the sole one used in
  production) populates the local IP and subnet only. It never sets SSID,
  BSSID or gateway, so a rule that matches solely on `ssids`, `bssids` or
  `gateways` silently never matches anything — it is dead configuration. As of
  this document, config validation rejects such a rule outright rather than
  accepting it silently.
- Matching is therefore by local subnet (CIDR) only. **A subnet is not a
  unique identity.** `192.168.1.0/24` (and other factory-default ranges) is
  common enough that someone else's home network, a neighbour's router, or a
  coffee shop could plausibly hand out an address in the same range. If you
  rely on `cidrs` to mean "I am physically at home", change your router's LAN
  range away from its default so the subnet is actually distinguishing.
- Stronger identity — pinning to a specific Wi-Fi SSID or a gateway's MAC
  address (BSSID) — is planned but not implemented on any platform today.
  Treat any config that lists `ssids`/`bssids`/`gateways` as documentation of
  intent, not as something currently enforced; validation now requires those
  rules to also carry a working `cidrs` entry.
- Practical consequence: a device that's off an allowed subnet publishes
  nothing (fail closed, by design). A device that's on a subnet with the same
  range as your home network — even if it's not actually your home — is
  treated as "home" (fail open on identity). Choose your subnet accordingly.

## Reporting a vulnerability

Open a private report via
[GitHub Security Advisories](https://github.com/crs2007/In-a-Call-Notification/security/advisories/new)
for this repository. If that's not usable for you, open a regular
[GitHub issue](https://github.com/crs2007/In-a-Call-Notification/issues) —
please avoid including exploit details or live credentials in a public issue;
a short description and a way to reach you is enough to start a conversation.

## Disclosure: leaked broker credential (commit `4733015`)

A Mosquitto broker username/password pair for a specific local network was
committed in `internal/config/example.yaml` at commit `4733015` on `main`.
That value must be treated as public. `internal/config/example.yaml` has
since been scrubbed to reference the password only via
`${CALLMQTT_MQTT_PASSWORD}`, and CI (`.github/workflows/ci.yml`,
`check-example-config` job) now fails the build if a literal `mqtt.password`
or either of the leaked identifier strings reappears in that file.

Two remaining steps are broker- and machine-side, not something this
repository or a rotation date recorded here can confirm:

- Rotating (or deleting/recreating) the affected user on the Mosquitto
  broker.
- Updating the real `%APPDATA%\callmqtt\config.yaml` on each affected
  machine to the new secret via `${CALLMQTT_MQTT_PASSWORD}`.

Those are tracked as open checklist items in `TODO.md`'s Phase 0 and are
**not** confirmed done from anything visible in this repository — this file
records the incident and the code-level remediation, not that the credential
rotation itself has happened.

Git history on `main` was deliberately **not** rewritten for this incident:
the value is meant to be rotated at the broker regardless of whether it
stays in history, and a force-push on `main` costs more (rewritten SHAs,
disrupted clones/forks) than it saves once the secret is already assumed
public.
