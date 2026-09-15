# In a Call Notification

Detect when you're in a Zoom, Microsoft Teams or Slack call — and publish that
state to your local MQTT broker, so Home Assistant can turn on a "do not
disturb" light for exactly as long as the call lasts.

Microsoft Teams detection has been confirmed against a real live call (see
[docs/TASKS.md](docs/TASKS.md), T38); Zoom and Slack are covered by recorded
fixtures but not yet live-confirmed the same way.

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

## Installation

Download the latest release from the
[releases page](https://github.com/crs2007/In-a-Call-Notification/releases/latest).
Releases ship as `callmqtt_<version>_windows_amd64.zip` or
`callmqtt_<version>_windows_arm64.zip` — pick the one matching your CPU
architecture, optionally verify it against the accompanying `checksums.txt`,
extract it, and run `callmqtt.exe`.

## License

MIT — see [LICENSE](LICENSE).
