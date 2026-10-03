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
| `meet-landing-page.txt` | Chrome on meet.google.com, no meeting opened. Browser otherwise idle. |
| `meet-lobby.txt` | Starts on the landing page, then opens a meeting link to the "Ready to join?" screen, camera preview visible, **not** joined. |
| `meet-in-call.txt` | Joined to the same Google Meet, Meet tab in front. |
| `meet-left.txt` | Just after clicking Leave, staying on the "You left the meeting" page. |

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
  stream open right now (WASAPI session in the Active state), with its
  instantaneous peak level; a paused player is not listed. **No rule scores
  this yet.** It was added hoping to separate a joined Google Meet from its
  "Ready to join?" lobby, and the captures showed it does not: Chrome opens
  the stream as soon as the Meet page loads (`meet-lobby.txt` snapshot 32),
  so it is present in lobby and call alike. The peak *did* differ (0.000
  throughout the lobby, brief spikes in the call), but a 2-second point
  sample is too sparse to score — a future rule would need a sampler. The
  section stays so future captures record it; the test parser skips it.
- The three `meet-*` captures with a meeting have the real meeting code
  replaced by `abc-defg-hij` (same shape). Nothing else in them is edited.
