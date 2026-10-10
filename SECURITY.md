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
- VPNs and tunnels: on Windows only operationally-up Ethernet and Wi-Fi
  adapters count as evidence; tunnel, PPP and other adapter types are ignored,
  as are well-known VPN adapter names and the CGNAT range `100.64.0.0/10`
  (Tailscale and similar). Off Windows only the name list applies.
- Very wide CIDRs: a prefix shorter than `/8` (IPv4) or `/32` (IPv6) — such
  as `0.0.0.0/0`, `::/0` or `2000::/3` — identifies no network at all and is
  rejected by config validation. A prefix shorter than `/16` (IPv4) or `/48`
  (IPv6) is accepted but logs a warning at startup and in
  `--validate-config`, and the tray shows the matching rule as
  "(wide rule)". The private ranges `10.0.0.0/8`, `172.16.0.0/12` and
  `192.168.0.0/16` are the ones every hotel, office and phone hotspot also
  uses, so a rule that wide treats almost anywhere as home. Allow the exact
  subnet you are on (the tray's "Allow current network" does this) rather
  than widening a rule until it matches.

### macOS

- **Broker password storage.** Environment variables do not reach an app
  started from Finder or at login, so on macOS `mqtt.password` can be
  `keychain:callmqtt`: the agent reads the generic-password item with service
  `callmqtt` and account = `mqtt.username` from the user's Keychain, and the
  secret never touches `config.yaml`. The item is created by the user
  (`security add-generic-password -U -s callmqtt -a <username> -w`). Any
  process running as the same user that is allowed to read that Keychain item
  can read the password; the Keychain does not protect it from other software
  you have already trusted with your login session. `keychain:` references
  are macOS-only. A literal password in `config.yaml` still works but is
  logged as a warning, as on Windows.
- **LaunchAgent.** `callmqtt startup enable` writes a per-user LaunchAgent,
  `~/Library/LaunchAgents/io.github.crs2007.callmqtt.plist` (label
  `io.github.crs2007.callmqtt`), that launches the executable path recorded at
  enable time (plus `--config` if you passed one). It holds no secrets. It runs
  with your privileges only, but anything that can write to your
  `~/Library/LaunchAgents` can already run code as you, so the plist is not an
  extra escalation path. `startup status` reports a stale entry if the app is
  moved.
- **Privacy permissions (TCC).** The agent needs **Screen Recording** only to
  read window *titles*; it does not capture, store or transmit screen
  contents. Without it, detection falls back to process names and per-app
  microphone evidence. It needs no Microphone permission (Core Audio reports
  which process is capturing audio and the agent never opens the microphone)
  and no Accessibility permission. Window titles can contain meeting names;
  they are matched locally and never published or logged, and `--probe`
  output (which does contain them) is only written to disk when you run it.
- **Unsigned binary.** The macOS app is not Developer ID signed or notarized
  (ad-hoc signed only), so macOS cannot vouch for who built it. Verify the
  download against `checksums.txt` from the same release before clearing the
  quarantine flag or choosing **Open Anyway**. TCC grants are tied to the
  app's code identity, so an update of an unsigned build may ask for Screen
  Recording again. Developer ID signing and notarization are wired into the
  release workflow but stay off until the signing secrets exist.
- **Reduced evidence on older macOS.** Before macOS 14.2 the agent cannot tell
  which app holds the microphone, only that one does, and camera use is always
  device-level; detection there is correspondingly weaker.

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
