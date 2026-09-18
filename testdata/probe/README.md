# Probe captures

Raw OS-signal dumps produced by `cmd/probe`. These are the **fixtures the
entire detection layer is tested against** — detection rules in
`internal/rules/rules.yaml` must be written from what is actually in these
files, never from guesswork about window title formats.

## How to capture

Run the probe, do the thing, then stop it with Ctrl-C:

```powershell
go run ./cmd/probe > testdata/probe/<scenario>.txt
```

Let each capture run for ~20 seconds so it contains several snapshots.

## Required scenarios

| File | What to do while it runs |
|---|---|
| `idle.txt` | Nothing. No call apps open at all. |
| `teams-open-no-call.txt` | Teams open, sitting on a **chat** and then a **calendar** view. Not in a call. |
| `teams-in-call.txt` | Actually joined to a Teams meeting, mic live. |
| `teams-in-call-muted.txt` | Same meeting, muted, camera off. |
| `zoom-open-no-call.txt` | Zoom running in the background, no meeting joined. |
| `zoom-in-call.txt` | Actually in a Zoom meeting. |
| `slack-huddle.txt` | In a Slack huddle. |
| `music-playing.txt` | Spotify or similar playing. No call app in a call. |
| `meet-lobby.txt` | *(not yet captured)* Google Meet "Ready to join?" screen open in the browser, camera preview visible, **not** joined. Then switch to another tab in the same window for a few snapshots. |
| `meet-in-call.txt` | *(not yet captured)* Joined to a Google Meet with at least one other participant, Meet tab in front. Then switch to another tab in the same window for a few snapshots. |
| `meet-left.txt` | *(not yet captured)* Just after leaving the Meet, on the "You left the meeting" page. |

The four "no call" captures matter **more** than the "in call" ones — they are
what proves the red bulb won't lie.

## What to look for

- `[windows]` — does an in-call window have a title that a *no-call* window
  doesn't? For New Teams this may not be true; if so, say so rather than
  stretching a regex to fit.
- `[microphone]` — an entry here means `LastUsedTimeStop == 0`, i.e. that app
  holds the mic **right now**. Expect the meeting app to appear in the in-call
  captures and disappear in the others.
- Music playback uses audio *output*, so a media player should **not** appear
  under `[microphone]` at all.
- `[audio-out]` — an entry here means that process has an audio *playback*
  stream running right now (WASAPI session in the Active state); a paused
  player is not listed. This is the signal that separates a joined Google
  Meet (the browser is rendering call audio) from its "Ready to join?" lobby
  (same tab title, same mic grant, nothing playing). Expect the browser here
  in `meet-in-call.txt` and **not** in `meet-lobby.txt`. Fixtures captured
  before this section existed simply have no `[audio-out]` block, which the
  test parser reads as "no playback evidence".
