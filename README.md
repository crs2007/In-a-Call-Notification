# CallMQTT

Detect when you're in a Zoom, Microsoft Teams or Slack call — and publish that
state to your local MQTT broker, so Home Assistant can turn on a "do not
disturb" light for exactly as long as the call lasts.

> 🚧 **Status: early development.** See [docs/](docs/) for the design research
> and the execution plan. Not yet usable.

## Why

There is no vendor-neutral desktop API that says "the user is currently in a
call". CallMQTT combines several weak local signals — running processes, window
titles, and which app currently holds the microphone — into a confidence score,
debounces it, and publishes a single reliable `active` / `inactive` state.

**A false positive is the worst failure mode.** A light that turns red when
you're merely listening to music, or that stays red after the call ended, makes
the whole signal untrustworthy. The detection is tuned to under-trigger rather
than over-trigger.

## Privacy

Everything runs locally. The agent publishes only the call state, the app name
and a confidence score. It never publishes meeting titles, participant names or
any conversation content, and it only publishes at all while you are connected
to a network you have explicitly allowed.

## License

MIT — see [LICENSE](LICENSE).
