# macOS probe captures

Fixtures for `internal/rules/rules_darwin.yaml`, taken with

```sh
callmqtt --probe --probe-out testdata/probe/darwin/<scenario>.txt
```

from Terminal.app with Screen Recording granted (15 snapshots, 2s apart).
Same contract as `../README.md`: no rule pattern without a line in one of
these files.

Captured 2026-10-16 on macOS 26.6, Apple Silicon, Zoom 7.2.2, Spotify 1.3.4.

| File | Scenario |
|---|---|
| `idle.txt` | No call apps open. |
| `music-playing.txt` | Spotify playing, no call app. |
| `zoom-open-no-call.txt` | Zoom running (at its sign-in window), no meeting. |
| `zoom-in-call.txt` | In a Zoom meeting joined from a browser link, mic and camera live. |

Not captured yet (Teams and Slack were not installed): `teams-open-no-call`,
`teams-in-call`, `teams-in-call-muted`, `slack-open-no-huddle`,
`slack-huddle`. Until they exist the macOS Teams and Slack rules have no
patterns.

## Format differences from Windows

- `proc=` is the executable base name (`zoom.us`, not `Zoom.exe`).
- Untitled windows are listed (`title=""`): without Screen Recording every
  title is empty and the window is still evidence that its process is on
  screen.
- `[microphone]` lists bundle IDs (`us.zoom.xos`) from Core Audio's
  per-process input state. Output-only audio (music) never appears there.
- `[webcam]` is device level only: `(unknown)` means some process has the
  camera.

## Redactions

Song titles, VS Code chat titles and project names were replaced with
`<… redacted>` placeholders. Nothing else was changed.
