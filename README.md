# In a Call Notification

[![CI](https://img.shields.io/github/actions/workflow/status/crs2007/In-a-Call-Notification/ci.yml?branch=main&label=CI)](https://github.com/crs2007/In-a-Call-Notification/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/crs2007/In-a-Call-Notification?include_prereleases)](https://github.com/crs2007/In-a-Call-Notification/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[![Home Assistant](https://img.shields.io/badge/Home_Assistant-MQTT_Discovery-41BDF5?logo=homeassistant&logoColor=white)](#home-assistant-integration)
[![MQTT](https://img.shields.io/badge/MQTT-v5-660066?logo=mqtt&logoColor=white)](https://www.home-assistant.io/integrations/mqtt/)
[![Windows](https://img.shields.io/badge/Windows-10%20%2F%2011-0078D4)](#installation)
[![Go](https://img.shields.io/github/go-mod/go-version/crs2007/In-a-Call-Notification?logo=go&logoColor=white)](go.mod)

Detect when you're in a Zoom, Microsoft Teams, Slack or Google Meet call — and
publish that state to your local MQTT broker, so Home Assistant can turn on a
"do not disturb" light for exactly as long as the call lasts.

Microsoft Teams detection has been confirmed against a real live call (see
[docs/TASKS.md](docs/TASKS.md), T38); Zoom and Slack are covered by recorded
fixtures but not yet live-confirmed the same way. Google Meet (in a browser,
no extension needed) is the newest detector — see
[Google Meet](#google-meet) for how it works and what it cannot tell apart.

## Table of Contents

- [Why](#why)
- [Privacy](#privacy)
- [What Home Assistant sees](#what-home-assistant-sees)
- [Home Assistant Integration](#home-assistant-integration)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Google Meet](#google-meet)
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
instead of lying if the machine dies mid-call. What to set up on the Home
Assistant side, and an automation to drive a light from it, are in
[Home Assistant Integration](#home-assistant-integration).

On the desktop side the agent lives in the system tray: the menu shows the
current state, the matched network and the broker connection, and lets you
pause detection, allow the current network, edit broker settings, and enable
start-at-login.

## Home Assistant Integration

[![Home Assistant](https://img.shields.io/badge/Home_Assistant-41BDF5?style=flat-square&logo=homeassistant&logoColor=white)](https://www.home-assistant.io/)
[![Mosquitto](https://img.shields.io/badge/Mosquitto_broker-3C5280?style=flat-square&logo=eclipsemosquitto&logoColor=white)](https://www.home-assistant.io/integrations/mqtt/#broker-configuration)
[![MQTT](https://img.shields.io/badge/MQTT_integration-660066?style=flat-square&logo=mqtt&logoColor=white)](https://www.home-assistant.io/integrations/mqtt/)

The agent talks to Home Assistant only through MQTT, so the whole integration
is: a broker, the MQTT integration, and one automation.

### What you need

1. **An MQTT broker** reachable from both the Windows PC and Home Assistant.
   The easiest is the official **Mosquitto broker** add-on: *Settings →
   Add-ons → Add-on Store → Mosquitto broker → Install → Start*. See
   [Broker configuration](https://www.home-assistant.io/integrations/mqtt/#broker-configuration)
   in the Home Assistant docs. Any other broker works too (standalone
   Mosquitto, EMQX, …) as long as it supports MQTT v5, which the agent
   requires — Mosquitto 1.6+ and the add-on both do.

2. **A Home Assistant user for the agent.** The Mosquitto add-on
   authenticates MQTT clients against Home Assistant users, so create one:
   *Settings → People → Users → Add user*. It does not need to be an
   administrator, and "can only log in from the local network" is fine.
   Its username and password go into `mqtt.username` / `mqtt.password`.

3. **The MQTT integration**: *Settings → Devices & services → Add
   Integration → MQTT*. With the add-on installed, Home Assistant discovers
   the broker and offers the connection automatically. Leave *Enable
   discovery* on and the discovery prefix at its default `homeassistant` —
   it must match `mqtt.discovery.prefix` in the agent config.

4. **The agent**, pointed at the broker: `mqtt.host` is the Home Assistant
   machine's IP (port `1883`), the credentials are the user from step 2, and
   at least one `allowed_networks` rule is set — see
   [Quick Start](#quick-start).

### What appears

Once the agent connects it publishes a retained discovery message to
`homeassistant/binary_sensor/<device_id>/call/config`, and Home Assistant
creates:

| | |
| --- | --- |
| Device | **In a Call Notification `<device_id>`** (manufacturer *In a Call Notification*, model *Desktop call presence*) |
| Entity | `binary_sensor.callmqtt_<device_id>_call`, device class `sound` — shown as **Detected** / **Clear** |
| State | `on` while the payload's `state` is `active`, `off` while `inactive`, `unknown` while it is `unknown` (the agent has just started and not decided yet — at most one exit debounce, about 8 s) |
| Attributes | `app`, `confidence`, `network`, `device`, `timestamp` (from the same JSON payload) |
| Availability | `online` / `offline` on the availability topic, with an MQTT last-will; the entity also expires to `unavailable` if no state arrives for 1.5 × `poll.heartbeat_seconds` (90 s by default) |

The discovery message is re-sent on every reconnect, so if you delete the
entity in Home Assistant, restarting the agent brings it back. Set
`mqtt.discovery.enabled: false` if you would rather define the sensor
yourself — if you do, keep the `unknown` → `None` mapping in the
`value_template` (`{{ 'None' if value_json.state == 'unknown' else value_json.state }}`),
or Home Assistant will ignore the agent's startup announcement and a
retained `active` left by a crash mid-call will keep the entity `on` until
the agent decides otherwise.

### Automation template

Paste this via *Settings → Automations & Scenes → Create Automation → Create
new automation → ⋮ → Edit in YAML*, then replace `my_laptop` with your
`device_id` and `light.office_busy` with your light. It uses two triggers so
the light also goes off when the entity becomes `unavailable` (laptop asleep
or dead), not only on a clean `off`:

```yaml
alias: Busy light follows calls
description: Red light while binary_sensor.callmqtt_my_laptop_call is on
mode: restart
triggers:
  - trigger: state
    entity_id: binary_sensor.callmqtt_my_laptop_call
    to: "on"
    id: call_started
  - trigger: state
    entity_id: binary_sensor.callmqtt_my_laptop_call
    to:
      - "off"
      - unavailable
      - unknown
    id: call_ended
actions:
  - choose:
      - conditions:
          - condition: trigger
            id: call_started
        sequence:
          - action: light.turn_on
            target:
              entity_id: light.office_busy
            data:
              color_name: red
              brightness_pct: 100
      - conditions:
          - condition: trigger
            id: call_ended
        sequence:
          - action: light.turn_off
            target:
              entity_id: light.office_busy
```

The attributes are available in templates. For example, add this to the
`call_started` sequence to get a phone notification naming the app:

```yaml
          - action: notify.mobile_app_my_phone
            data:
              title: In a call
              message: >-
                {{ trigger.to_state.attributes.app | title }} call started
                ({{ (trigger.to_state.attributes.confidence * 100) | round }}% confidence)
```

The syntax above needs Home Assistant 2024.10 or newer; on older versions
use `trigger:` / `platform: state` / `service:` instead of `triggers:` /
`trigger: state` / `action:`.

### Checking it works

- Run `.\callmqtt.exe --simulate` on the PC — it fakes a call every 30
  seconds, so the entity and the automation can be tested without joining a
  meeting.
- In Home Assistant, open the MQTT integration → *Configure* → *Listen to a
  topic* and subscribe to `desktop-presence/#` to see the raw JSON payloads,
  or `homeassistant/binary_sensor/#` to see the discovery config.
- If nothing arrives at all, it is almost always `allowed_networks` — see
  [FAQ / Troubleshooting](#faq--troubleshooting).

## Installation

Windows 10/11 only. Download the latest release from the
[releases page](https://github.com/crs2007/In-a-Call-Notification/releases/latest).
Releases ship as two archives:

| Archive | For |
|---|---|
| `callmqtt_<version>_windows_amd64.zip` | Almost every PC: Intel or AMD processors |
| `callmqtt_<version>_windows_arm64.zip` | Windows on ARM (Snapdragon-based laptops, Surface Pro X) |

Not sure? **Settings → System → About → System type** says which. Extract the
archive anywhere; it contains `callmqtt.exe`, `configs/example.yaml`, this
README and the LICENSE. To check the download against `checksums.txt` from the
same release:

```powershell
Get-FileHash .\callmqtt_<version>_windows_amd64.zip -Algorithm SHA256
```

The executables are not code-signed yet, so the first launch may show a
Windows SmartScreen "Windows protected your PC" prompt: click **More info**,
then **Run anyway**. Verifying the checksum first is what makes that a safe
click.

To build from source instead (Go 1.27+):

```powershell
go build -tags tray ./cmd/callmqtt    # the tray app that ships in releases
go build ./cmd/callmqtt               # headless console build, useful for debugging
```

## Quick Start

1. Start `callmqtt.exe` once — double-clicking it is fine. With no config
   yet, it writes a starter one to `%APPDATA%\callmqtt\config.yaml`, shows a
   message saying so, and exits. (From a terminal, `.\callmqtt.exe init`
   does the same and prints the next steps.)

2. Edit that file. The minimum you must change is the broker and the network
   you want to publish from:

   ```yaml
   mqtt:
     host: 192.168.1.10          # your MQTT broker
     username: homeassistant
     password: ${CALLMQTT_MQTT_PASSWORD}  # reads the environment variable; avoids a literal secret

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
   the MQTT integration automatically. Broker setup and a ready-made
   automation are in
   [Home Assistant Integration](#home-assistant-integration).

5. To test the automation without joining a real call, run
   `.\callmqtt.exe --simulate` — it fakes a call every 30 seconds.

## Configuration

Config lives at `%APPDATA%\callmqtt\config.yaml` (override with `--config`).
`configs/example.yaml` in the release archive documents every key; the
important ones and their defaults:

| Key | Default | What it controls |
| --- | --- | --- |
| `app.device_id` | `auto` (from hostname) | Appears in the MQTT topics and the Home Assistant entity id. With `auto`, a hostname containing no ASCII letters or digits (for example Hebrew, Cyrillic or CJK) yields `unknown-host`. If several such machines share one broker, set `app.device_id` explicitly on each so their topics and entities don't collide. |
| `mqtt.host` / `mqtt.port` | — / `1883` | Broker address. `host` is required: a host name or IP only (IPv6 literals allowed), no scheme or port. |
| `mqtt.username` / `mqtt.password` | — | Broker credentials. `password` may be `${ENV_VAR}`. |
| `mqtt.client_id` | `callmqtt-<device_id>-<6 hex>` | MQTT client id. The 6-hex suffix is a random per-install id stored in `%APPDATA%\callmqtt\install_id`, so machines with duplicate hostnames, or two sessions, don't take over each other's broker session. Topics and the Home Assistant entity use the plain `device_id`, without the suffix. The client id changes once when you upgrade; the old persistent session is left orphaned on the broker, which is harmless. |
| `mqtt.qos` / `mqtt.retain` | `1` / `true` | Publish options for state messages. |
| `mqtt.tls.enabled` | `false` | Enable TLS to the broker (`insecure_skip_verify` also available). |
| `mqtt.discovery.enabled` / `.prefix` | `true` / `homeassistant` | Auto-create the Home Assistant `binary_sensor`; the prefix must match the MQTT integration's discovery prefix. See [Home Assistant Integration](#home-assistant-integration). |
| `topics.state` | `desktop-presence/{device_id}/call` | Where the JSON state payload is published. Must be a plain topic name: no `+` or `#` wildcards, no leading `$`. |
| `topics.availability` | `desktop-presence/{device_id}/availability` | `online` / `offline`, with an MQTT last-will. Same topic-name rules as `topics.state`. |
| `allowed_networks` | *(empty — publishes nothing)* | Rules matched by `ssids`, `bssids`, `cidrs` or `gateways`. **Only `cidrs` currently matches anything** — `ssids`/`bssids`/`gateways` are accepted by the schema but not yet implemented on any platform; a rule relying on them alone fails config validation. A CIDR shorter than `/8` (IPv4) or `/32` (IPv6), such as `0.0.0.0/0`, is rejected; one shorter than `/16` or `/48` (e.g. `10.0.0.0/8`) is accepted but logs a warning, and the tray marks the rule "(wide rule)". See [Privacy](#privacy). |
| `detectors.<teams\|zoom\|slack\|meet>.enabled` | `true` | Turn individual app detectors on or off. `meet` is Google Meet in a browser; see [Google Meet](#google-meet). |
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
| `callmqtt startup enable\|disable\|status` | Manage start-at-login. A custom `--config` is kept in the login entry; if the install folder is moved, `status` and the tray checkbox show it as not enabled until you enable it again. |
| `--config <path>` | Use a different config file. |
| `--validate-config` | Check the config and exit. |
| `--print-config` | Print the effective config, secrets redacted, and exit. |
| `--once` | Run one detection cycle, print the result, and exit. |
| `--simulate` | Fake a call every 30 s to test a Home Assistant automation. |
| `--debug` | Log each detector's score, confidence and reasons on every poll, to stderr and the log file. Window titles and process names are never logged. |
| `--probe` | Take `--count` snapshots (default 15, 2 s apart) of visible window titles and microphone/camera owners, and write them to `probe-<timestamp>.txt` next to the config (or `--probe-out <file>`). The output contains window titles; review it before sharing. |
| `--version` | Print the version and exit. |

Settings changed from the tray menu (broker, discovery, allowed network) are
saved to `config.yaml` and applied live. Edits you make to the file by hand
take effect on the next start.

## Google Meet

Google Meet runs in a browser tab, so there is no process to watch and no
local API to ask. The `meet` detector needs **no browser extension** and no
extra Windows permission: it combines three signals the agent already reads
for the other apps, all from the OS side of the browser.

| Signal | Where it comes from | What it means for Meet |
| --- | --- | --- |
| Window title | `EnumWindows` on the browser's top-level windows (Chrome, Edge, Brave, Firefox, Vivaldi, Opera, and Chrome/Edge "installed app" windows) | A window whose *active tab* is titled `Meet – abc-defg-hij` or `Meet – <calendar event>`. |
| Microphone | The same registry `ConsentStore` used for Teams/Zoom/Slack | The browser currently holds the microphone. |
| Audio playback | WASAPI audio sessions (`IAudioSessionManager2`) | The browser has an audio output stream running right now. |

Each signal is worth 0.25. A joined call with the Meet tab in front shows all
three (0.75, above the 0.70 threshold). The reason for the third signal is the
**"Ready to join?" lobby**: its tab title is identical to the call's, and it
holds the mic and camera for the self-view preview — but it plays no audio. A
real WebRTC call keeps an output stream open for its whole duration, the lobby
does not, so the lobby scores 0.50 and stays dark.

Once a call is active, switching to another tab in the same window (which
hides the Meet title) does not drop it: mic + playback (0.50) stays above the
0.30 `inactive_threshold`, so the light holds until the browser releases the
microphone. No single signal is worth 0.30 on its own, which is what stops a
lingering `Meet – …` tab after you leave, a YouTube tab, or a bare mic grant
from keeping (or starting) a call.

Known limits, by design:

- Waiting in the lobby **while another tab of the same browser plays audio**
  looks like a call (0.75) until you join or close the lobby.
- **Alone in a meeting** nobody else has joined, the browser may have no
  output stream yet, so the light can stay off until a second participant's
  audio arrives. That is the accepted side of the under-trigger tuning.
- The `meet` rule is not yet backed by a `testdata/probe/meet-*.txt` capture;
  its title pattern comes from Meet's documented tab titles and the browser
  window titles present in the other captures. If it misses your call, run
  `go run ./cmd/probe` during a real Meet (see
  [testdata/probe/README.md](testdata/probe/README.md)) and open an issue with
  the capture — the fix is usually one line in `rules.yaml`.

No `UIAutomation`, accessibility, or screen-recording permission is involved:
window titles, the ConsentStore and audio sessions are all readable by a
normal user account. If Windows ever refuses one of them, that signal simply
contributes nothing and the detector reports `inactive` rather than failing.

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

Every push to `main` is a release: the Release workflow computes the next
semver tag (patch bump by default; `[minor]` / `[major]` in the commit subject
for a larger bump, `[skip release]` to publish nothing), pushes the tag, and
GoReleaser builds and publishes the archives. Open an issue first for anything beyond a rule tweak so the approach
can be agreed before you write code.

## FAQ / Troubleshooting

**The light never turns on, but the tray shows a call.**
Almost always `allowed_networks`. An empty list publishes nothing, and a
CIDR that doesn't match your current subnet does the same. The tray menu shows which network rule (if
any) currently matches; `callmqtt --once` prints `publishing: true|false` and
the reasons. Fix it by allowing the exact subnet you are on (the tray's
"Allow current network"), not by widening an existing CIDR until it matches:
a range like `10.0.0.0/8` also matches hotel, office and hotspot networks.

**Double-clicking `callmqtt.exe` seems to do nothing.**
It should never be silent: with no config it writes a starter one and says
where; any other startup failure is shown in a message box. If you truly see
nothing, SmartScreen may have blocked the launch — see
[Installation](#installation) — or check `%APPDATA%\callmqtt\callmqtt.log`.

**`--once` / `--validate-config` print nothing.**
The release binary is a Windows GUI app (no console window), so it cannot
write to the terminal. `init` and startup errors show a message box instead;
for the inspection flags, use the headless build: `go build ./cmd/callmqtt`.

**The light stays on after my laptop went to sleep or died.**
Expected for up to 1.5 × `poll.heartbeat_seconds` (90 s by default), after
which Home Assistant marks the entity unavailable. The availability topic's
last-will also flips to `offline` as soon as the broker notices the connection
drop. Lower `heartbeat_seconds` if you need faster recovery.

**The log says "another client is using this client_id".**
The broker sent DISCONNECT 0x8E (session taken over): two clients connected with
the same `mqtt.client_id` and each keeps kicking the other off. Give each
machine a unique `mqtt.client_id`, or a unique `app.device_id` (the default
client id is derived from it). This is most likely when several machines share
the `unknown-host` device id.

**The log warns that my MQTT password is written in the config.**
Set `mqtt.password: ${CALLMQTT_MQTT_PASSWORD}` and define that environment
variable for your user instead of storing the secret in plain text.

**Zoom or Slack calls are not detected.**
Those detectors are validated against recorded fixtures but have not yet been
confirmed on a live call. While you are in a real call, run
`callmqtt --probe` and open the `probe-<timestamp>.txt` file it writes next to
your config. It lists the process names and window titles on your machine, so
redact anything private, then attach it to an issue — the fix is usually a rule
in `rules.yaml`. `--debug` adds the detectors' scores to the log, which shows
why a call scored too low, but not the titles.

**Google Meet is not detected, or only once someone else joins.**
See [Google Meet](#google-meet): the detector needs the browser's window
title, its microphone grant *and* an audio output stream, and the last one
only appears once there is remote audio to play. If you are the only
participant the light may stay off; if it never turns on at all, run
`go run ./cmd/probe --count 3` during a call and check that a browser window
titled `Meet – …` is listed under `[windows]`, the browser under
`[microphone]`, and the browser's exe under `[audio-out]`. Whichever is
missing is the signal to report in an issue.

## License

MIT — see [LICENSE](LICENSE).
