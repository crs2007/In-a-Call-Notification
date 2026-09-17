# Security Policy

CallMQTT is a single-binary desktop agent; there is no supported-versions
matrix to publish here — the latest release is the only one that gets fixes.

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
