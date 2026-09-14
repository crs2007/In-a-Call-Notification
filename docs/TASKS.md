# CallMQTT — task tracker

Numbering continues from the commit history (T28 is the first commit that
carried a task number). Each task has a copy-paste prompt for starting it cold
in a new thread — self-contained, no reliance on this conversation's context.

Status legend: ✅ done and validated · ⬜ not started

---

## ✅ T28 — `internal/supervisor`: reload the engine and broker live
Commit: `8a27f7d`. Lets the engine and MQTT client be rebuilt on a config
change without restarting the process.
**Validated:** builds, `go test ./internal/supervisor/...` passes.

## ✅ T29 — Tray UI wired into main
Commit: `e395464`. `internal/tray` + `cmd/callmqtt/ui_tray.go` /
`ui_headless.go` give a real tray on Windows and a headless fallback
elsewhere.
**Validated:** builds on both `GOOS=windows` and `GOOS=darwin` (headless
stub), `go vet` clean.

## ✅ T30 — Win32 broker settings dialog
Commit: `5f5b148`. `platform/windows/dialog.go` +
`cmd/callmqtt/dialog_windows.go` / `dialog_stub.go`.
**Validated:** builds and vets clean on both platforms; stub keeps the
non-Windows build green.

## ✅ T31 — Extract Win32 signal-gathering into `platform/windows`
Commits: `222177c`. Moved `cmd/probe`'s EnumWindows window enumeration,
ConsentStore mic/webcam-in-use reads, and gopsutil process-name lookup into
`platform/windows/{windows.go,consent.go,consent_pure.go,process.go}`, with
`stub_other.go` keeping the package importable on non-Windows. `cmd/probe`
now only calls the adapter.
**Validated:**
- `go build ./...` and `GOOS=darwin go build ./...` both clean.
- `go test ./platform/windows/...` passes (pure key-unmangling and
  `LastUsedTimeStop`-liveness logic covered without needing a real registry).
- No CGO introduced; build tags present on every OS-specific file.

## ✅ T32 — Capture the remaining detection fixtures
Commit: `222177c` (bundled with T31). All 8 scenarios from
`testdata/probe/README.md` now exist under `testdata/probe/`: `idle.txt`,
`teams-open-no-call.txt`, `teams-in-call.txt`, `teams-in-call-muted.txt`,
`zoom-open-no-call.txt`, `zoom-in-call.txt`, `slack-huddle.txt`,
`music-playing.txt`.
**Validated:** files present and non-trivial (hundreds of lines each,
multiple snapshots); `internal/rules/rules.yaml` cites specific captured
lines from each one, so the rules were demonstrably written from these
fixtures rather than guessed.

## ✅ T33 — `internal/rules` schema + loader
Commit: `222177c`. YAML schema (process names, window include/exclude
regex, mic-process regex, per-signal weights) plus loader in
`internal/rules/rules.go`, embedded into the binary via `rules.Default()`
(commit `8b905e5`).
**Validated:** `go test ./internal/rules/...` passes; every regex in
`rules.yaml` is commented with the exact fixture line it came from, matching
the detector-rules agent's hard rule against guessed patterns. Correctly
keeps process-alone and mic-alone below the active threshold for all three
apps.

## ✅ T34 — Rule-based detector (`internal/detectors`)
Commit: `222177c`. Generic detector combining platform signals + rules into
`model.DetectionResult` per app.
**Validated:** `go test ./internal/detectors/...` passes.

## ✅ T35 — Wire real detectors into `cmd/callmqtt`
Commit: `8b905e5`. `buildDetectors` now builds real `model.Detector`s from
`rules.Default()` and the live Win32 snapshot instead of refusing to run;
`-simulate` still short-circuits to the fake detector.
**Validated:** `go build ./...` clean; `go test ./...` all green across
every package. **Not yet validated: an actual live run** (`callmqtt --once`
during a real Teams/Zoom call) — do that before calling detection
release-ready, and update README's "🚧 early development / not yet usable"
line once confirmed.

---

## ✅ T36 — GitHub Actions CI
Commit: `068d532`. Added `.github/workflows/ci.yml` with two jobs:
`test-windows` (gate — `go vet`, build with and without the `tray` tag,
`go test -race`, plus a `windows/arm64` build-only cross-compile) and
`cross-compile-darwin` on `ubuntu-latest` (`GOOS=darwin GOARCH=arm64 go
build`, ~20s, keeps the macOS stubs from rotting). Uses
`go-version-file: go.mod` so CI tracks the toolchain automatically.

**Premise correction:** the task prompt (copied from `docs/Project
review.md`) asked for `golangci-lint` and an amd64/arm64 cross-compile
matrix. The committed `.claude/agents/release-ci.md` spec — the more
current, explicit policy for this repo — says to omit golangci-lint at this
project size (`go vet` catches what matters) and to use windows-latest as
the sole real gate plus a cheap darwin cross-compile check, not a literal
amd64/arm64 matrix. Asked the user directly; they chose to follow
`release-ci.md` over the stale prompt text. No linter step was added.
**Validated:**
- `go build ./...`, `GOOS=darwin go build ./...`, `go test ./...` all clean.
- Also ran locally what the workflow runs: `go vet ./...`,
  `go build -tags tray ./...`, `GOOS=windows GOARCH=arm64 go build ./...` —
  all clean.
- `go test -race ./...` not run locally (no cgo/gcc in this sandbox); relies
  on GitHub's `windows-latest` image providing gcc on PATH, which is the
  standard way Go projects get `-race` working on Windows CI.
- YAML syntax validated (`yaml.safe_load`).

## ✅ T37 — Packaging with GoReleaser
Commit: `3a178ee`. Added `.goreleaser.yaml` and `.github/workflows/release.yml`
via the release-ci subagent.
- Builds `./cmd/callmqtt` with the `tray` build tag (the real user-facing
  binary — `gogpu/systray` is pure Go via `purego`, so `CGO_ENABLED=0` works
  for both `windows/amd64` and `windows/arm64`; the headless `!tray` build is
  only a CI compile-check, not something meant to ship).
- Ldflags: `-s -w -H=windowsgui -X main.version={{.Version}}` — wires the
  version var `cmd/callmqtt/main.go` already expects and suppresses the
  console flash a `-H=windowsgui` tray app would otherwise show.
  `internal/config/example.yaml` is bundled into the zip as
  `configs/example.yaml` alongside README.md and LICENSE, plus a
  `checksums.txt`.
- `release.yml` triggers on `v*` tags, runs on `windows-latest`, and calls
  `goreleaser/goreleaser-action@v6` with `release --clean`.
**Validated:**
- `goreleaser check` passed; `goreleaser release --snapshot --clean
  --skip=publish` actually built both windows/amd64 and windows/arm64
  binaries, produced correctly-shaped zip archives, and the built
  `callmqtt.exe -version` printed the expected snapshot version string —
  confirms the ldflag wiring is correct, not just that the YAML parses.
- `go build ./...`, `GOOS=darwin go build ./...`, `go test ./...` all clean
  (re-run directly, not just inside the subagent).
- `git status --short` after the subagent's run showed only the two new
  files — nothing else touched, no leftover `dist/`.

## ✅ T38 — Confirm real detection against a live call, then flip README's status
No single commit — a live-testing session plus a follow-on rule fix.

Ran `go run ./cmd/callmqtt --once` before, during, and after a real Microsoft
Teams call. The "before" reading immediately surfaced a genuine bug the unit
tests had missed: Teams' small, always-present "Meet" utility window (title
`Meet | Microsoft Teams`) matched the same `window_include_regex` as a real
meeting window, scoring `process (0.20) + window (0.55) = 0.75` — above the
0.70 active threshold — while Teams was completely idle. `testdata/probe/teams-open-no-call.txt`
never caught this because it happened to be captured while Teams was on its
Chat tab, not showing the bare "Meet" window.

Commit: `<fill in after commit>`. Fixed by the `detector-rules` subagent:
narrowed `internal/rules/rules.yaml`'s Teams `window_include_regex` from
`'^(Meet|Meeting with .+) \| Microsoft Teams$'` to
`'^Meeting with .+ \| Microsoft Teams$'` (bare "Meet" no longer matches at
all; weights unchanged), documented the finding inline, and added
`TestTeamsIdleMeetWindowOnly_IsInactive` in `internal/rules/rules_test.go` as
a synthetic regression.

**Premise correction:** the task assumed a single live pass would either
"hold up" or not. In practice the first live check found a real false
positive; T38 also covers verifying the fix, not just the original
detectors — the live-test loop was: idle (found bug, 0.75 confidence) → fix
applied → idle again (0.20) → in-call (1.00, all three signals) → after call
(back to 0.20). Only Microsoft Teams was live-tested; Zoom and Slack remain
fixture-verified only, so README now calls that out explicitly rather than
claiming the whole app is confirmed.

**Validated:**
- `go build ./...`, `GOOS=darwin go build ./...`, `go test ./...` all clean
  after the fix (re-run directly, not just inside the subagent).
- `internal/rules` and `internal/detectors` test suites pass, including the
  pre-existing acceptance-bar scenarios (`TestTeamsInCall_IsActive`,
  `TestTeamsMuteUnmute_NoStateChange`, `TestTeamsOpenNoCall_IsInactive`,
  `TestZoomInCall_IsActive`, `TestZoomOpenNoCall_IsInactive`,
  `TestSlackHuddle_DetectsIntermittently`,
  `TestMusicPlaying_IsInactiveForEveryApp`) plus the new regression.
- Live `go run ./cmd/callmqtt --once`, cross-checked against
  `go run ./cmd/probe --count 1`, at four points on a real machine with a
  real Teams call:
  - idle, before fix: confidence 0.75, reasons `[process present, meeting
    window title matched]` — wrongly above threshold.
  - idle, after fix: confidence 0.20, reasons `[process present]`.
  - during the call: confidence 1.00, reasons `[process present, meeting
    window title matched, microphone in use]`; probe confirmed both
    `Meet | Microsoft Teams` and `Meeting with Sharon Rimer | Microsoft
    Teams` windows plus `MSTeams_8wekyb3d8bbwe` holding the microphone.
  - after the call ended: confidence back to 0.20, `Meeting with ...` window
    and mic entry both gone.
- README.md's "🚧 early development... Not yet usable" line removed; replaced
  with a note scoping the claim to Teams being live-confirmed and Zoom/Slack
  remaining fixture-only.

**Follow-up spotted:** the same "always-present secondary window" hazard that
caused this Teams bug may exist for Zoom/Slack too and hasn't been
live-checked — worth a live pass on those before trusting their detection the
same way.

## ⬜ (unscoped) — macOS platform support
No owning subagent exists yet (`win-platform` only covers `platform/windows/`).
Worth revisiting once T36–T38 are done and Windows detection has proven itself
live, rather than starting it now.
