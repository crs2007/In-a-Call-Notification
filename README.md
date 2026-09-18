# In a Call Notification

[![CI](https://img.shields.io/github/actions/workflow/status/crs2007/In-a-Call-Notification/ci.yml?branch=main&label=CI)](https://github.com/crs2007/In-a-Call-Notification/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/crs2007/In-a-Call-Notification?include_prereleases)](https://github.com/crs2007/In-a-Call-Notification/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Detect when you're in a Zoom, Microsoft Teams or Slack call — and publish that
state to your local MQTT broker, so Home Assistant can turn on a "do not
disturb" light for exactly as long as the call lasts.

Microsoft Teams detection has been confirmed against a real live call (see
[docs/TASKS.md](docs/TASKS.md), T38); Zoom and Slack are covered by recorded
fixtures but not yet live-confirmed the same way.

## Table of Contents

- [Why](#why)
- [Privacy](#privacy)
- [What Home Assistant sees](#what-home-assistant-sees)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Contributing](#contributing)
- [FAQ / Troubleshooting](#faq--troubleshooting)
- [License](#license)

## Why

There is no vendor-neutral desktop API that says "the user is currently in a
call". In a Call Notification combines several weak local signals — running
processes, window titles, and which app currently holds the microphone — into a
confidence score, debounces it, and publishes a single reliable `active` /
`inactive` state.

**A false positive is the worst failure mode.** A light that turns red when
you're merely listening to music, or that stays red after the call ended, makes
the whole signal untrustworthy. The detection is tuned to under-trigger rather
than over-trigger.

## Privacy

Everything runs locally. The agent publishes only the call state, the app name
and a confidence score. It never publishes meeting titles, participant names or
any conversation content. It publishes nothing at all while off an allowed
network.

**Network matching today is by local subnet (CIDR) only.** The config schema
also accepts `ssids`, `bssids` and `gateways` in `allowed_networks`, but no
current platform implementation populates SSID, BSSID or gateway information —
those fields are planned, not shipped, and a rule that relies on them alone
fails config validation. A subnet is not a unique identity: `192.168.1.0/24`
is a common router default that someone else's home or a coffee shop could
plausibly share. Pick a non-default subnet for your home network (change your
router's LAN range from the factory default) so `allowed_networks` actually
distinguishes "home" from "somewhere with the same default subnet". Stronger
identity — matching a specific SSID or gateway MAC/BSSID — is aspirational,
not implemented; see
[SECURITY.md](SECURITY.md#threat-model--known-limitations) for the full
threat model.

## What Home Assistant sees

The agent publishes one retained JSON message to
`desktop-presence/<device_id>/call` whenever the state changes, and re-asserts
it on a heartbeat:

```json
{
  "device": "my-laptop",
  "state": "active",
  "app": "teams",
  "confidence": 0.92,
  "network": "Home",
  "timestamp": "2026-09-15T09:41:07+03:00"
}
```

With MQTT discovery enabled (the default) that becomes a
`binary_sensor.callmqtt_<device_id>_call` entity in Home Assistant — `on`
while `state` is `active`, with `app`, `confidence` and `network` as
attributes — plus an availability topic so the sensor goes `unavailable`
instead of lying if the machine dies mid-call.

On the desktop side the agent lives in the system tray: the menu shows the
current state, the matched network and the broker connection, and lets you
pause detection, allow the current network, edit broker settings, and enable
start-at-login.

## Installation

Windows 10/11 only (amd64 or arm64). Download the latest release from the
[releases page](https://github.com/crs2007/In-a-Call-Notification/releases/latest).
Releases ship as `callmqtt_<version>_windows_amd64.zip` or
`callmqtt_<version>_windows_arm64.zip` — pick the one matching your CPU
architecture, optionally verify it against the accompanying `checksums.txt`,
and extract it. The archive contains `callmqtt.exe`, `configs/example.yaml`,
this README and the LICENSE.

To build from source instead (Go 1.27+):

```powershell
go build -tags tray ./cmd/callmqtt    # the tray app that ships in releases
go build ./cmd/callmqtt               # headless console build, useful for debugging
```

## Quick Start

1. Write a starter config. It lands in `%APPDATA%\callmqtt\config.yaml`:

   ```powershell
   .\callmqtt.exe init
   ```

2. Edit that file. The minimum you must change is the broker and the network
   you want to publish from:

   ```yaml
   mqtt:
     host: 192.168.1.10          # your MQTT broker
     username: homeassistant
     password: ${MQTT_PASSWORD}  # reads the environment variable; avoids a literal secret

   allowed_networks:
     - name: Home
       cidrs: ["192.168.1.0/24"] # publish only while on this subnet
   ```

3. Start the agent. A tray icon appears; the menu shows what it detects and
   whether it is publishing. Tick **Start at login** to keep it running.

   ```powershell
   .\callmqtt.exe
   ```

4. In Home Assistant, `binary_sensor.callmqtt_<device_id>_call` appears under
   the MQTT integration automatically. A minimal automation:

   ```yaml
   automation:
     - alias: Busy light follows calls
       trigger:
         - platform: state
           entity_id: binary_sensor.callmqtt_my_laptop_call
       action:
         - service: "light.turn_{{ 'on' if trigger.to_state.state == 'on' else 'off' }}"
           target:
             entity_id: light.office_busy
   ```

5. To test the automation without joining a real call, run
   `.\callmqtt.exe --simulate` — it fakes a call every 30 seconds.

## Configuration

Config lives at `%APPDATA%\callmqtt\config.yaml` (override with `--config`).
`configs/example.yaml` in the release archive documents every key; the
important ones and their defaults:

| Key | Default | What it controls |
| --- | --- | --- |
| `app.device_id` | `auto` (from hostname) | Appears in the MQTT topics and the Home Assistant entity id. |
| `mqtt.host` / `mqtt.port` | — / `1883` | Broker address. `host` is required. |
| `mqtt.username` / `mqtt.password` | — | Broker credentials. `password` may be `${ENV_VAR}`. |
| `mqtt.client_id` | `callmqtt-<device_id>` | MQTT client id. |
| `mqtt.qos` / `mqtt.retain` | `1` / `true` | Publish options for state messages. |
| `mqtt.tls.enabled` | `false` | Enable TLS to the broker (`insecure_skip_verify` also available). |
| `mqtt.discovery.enabled` / `.prefix` | `true` / `homeassistant` | Auto-create the Home Assistant `binary_sensor`. |
| `topics.state` | `desktop-presence/{device_id}/call` | Where the JSON state payload is published. |
| `topics.availability` | `desktop-presence/{device_id}/availability` | `online` / `offline`, with an MQTT last-will. |
| `allowed_networks` | *(empty — publishes nothing)* | Rules matched by `ssids`, `bssids`, `cidrs` or `gateways`. **Only `cidrs` currently matches anything** — `ssids`/`bssids`/`gateways` are accepted by the schema but not yet implemented on any platform; a rule relying on them alone fails config validation. See [Privacy](#privacy). |
| `detectors.<teams\|zoom\|slack>.enabled` | `true` | Turn individual app detectors on or off. |
| `detection.active_threshold` / `.inactive_threshold` | `0.70` / `0.30` | Confidence needed to enter / leave the `active` state. |
| `detection.enter_debounce_seconds` / `.exit_debounce_seconds` | `2` / `8` | Asymmetric on purpose: quick to light up, slow to go dark. |
| `poll.detect_seconds` / `.network_seconds` | `2` / `10` | How often signals and the current network are sampled. |
| `poll.heartbeat_seconds` | `60` | State re-publish interval; Home Assistant expires the entity after 1.5× this. |
| `rules_file` | *(built-in)* | Path to an alternative `rules.yaml` of detection rules. |
| `logging.level` / `logging.file` | `info` / `%APPDATA%\callmqtt\callmqtt.log` | Log verbosity and destination. |

### Command line

| Command / flag | Purpose |
| --- | --- |
| `callmqtt` | Run the agent (tray icon in the release build). |
| `callmqtt init` | Write a starter config and print where it went. |
| `callmqtt startup enable\|disable\|status` | Manage start-at-login. |
| `--config <path>` | Use a different config file. |
| `--validate-config` | Check the config and exit. |
| `--print-config` | Print the effective config, secrets redacted, and exit. |
| `--once` | Run one detection cycle, print the result, and exit. |
| `--simulate` | Fake a call every 30 s to test a Home Assistant automation. |
| `--debug` | Log every poll to stderr. |
| `--version` | Print the version and exit. |

Settings changed from the tray menu (broker, discovery, allowed network) are
saved to `config.yaml` and applied live. Edits you make to the file by hand
take effect on the next start.

## Contributing

**Adding support for a new app means editing
[internal/rules/rules.yaml](internal/rules/rules.yaml), not writing Go.**
Detection rules are process names, window-title patterns and signal weights;
the detector engine is generic.

Development setup is plain Go, and mirrors CI:

```powershell
go vet ./...
go build ./...                 # headless build
go build -tags tray ./...      # tray build, as shipped
go test -race ./...
```

Releases are cut by pushing a `v*` tag; GoReleaser builds and publishes the
archives. Open an issue first for anything beyond a rule tweak so the approach
can be agreed before you write code.

## FAQ / Troubleshooting

**The light never turns on, but the tray shows a call.**
Almost always `allowed_networks`. An empty list publishes nothing, and a
mismatched CIDR/SSID does the same. The tray menu shows which network rule (if
any) currently matches; `callmqtt --once` prints `publishing: true|false` and
the reasons.

**`callmqtt init` / `--once` / `--validate-config` print nothing.**
The release binary is a Windows GUI app (no console window), so it cannot
write to the terminal — `init` still creates the file and startup errors are
shown in a message box. For console output, use the headless build:
`go build ./cmd/callmqtt`.

**The light stays on after my laptop went to sleep or died.**
Expected for up to 1.5 × `poll.heartbeat_seconds` (90 s by default), after
which Home Assistant marks the entity unavailable. The availability topic's
last-will also flips to `offline` as soon as the broker notices the connection
drop. Lower `heartbeat_seconds` if you need faster recovery.

**The log warns that my MQTT password is written in the config.**
Set `mqtt.password: ${MQTT_PASSWORD}` and define that environment variable for
your user instead of storing the secret in plain text.

**Zoom or Slack calls are not detected.**
Those detectors are validated against recorded fixtures but have not yet been
confirmed on a live call. Run with `--debug`, capture the process names and
window titles it sees during a real call, and open an issue — the fix is
usually a rule in `rules.yaml`.

## License

MIT — see [LICENSE](LICENSE).
