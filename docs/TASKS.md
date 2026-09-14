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
Commit: `<pending>`. Added `.github/workflows/ci.yml` with two jobs:
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

## ⬜ T37 — Packaging with GoReleaser
Depends on T36.
```
In the CallMQTT repo (E:\GitHub\In-a-Call-Notification), add a .goreleaser.yaml to produce
tagged Windows release binaries (amd64/arm64) with the version ldflag main.go already expects
(`-ldflags "-X main.version=..."`). Wire a release.yml GitHub Actions workflow that runs
GoReleaser on a version tag push. Keep scope to Windows only for now, matching the current
platform support. Use the release-ci subagent for this.
```

## ⬜ T38 — Confirm real detection against a live call, then flip README's status
Depends on T35 being live-tested, not just unit-tested.
```
In the CallMQTT repo (E:\GitHub\In-a-Call-Notification), the real detectors (internal/rules +
internal/detectors, wired into cmd/callmqtt/main.go's buildDetectors) pass all unit tests
against recorded fixtures but have not been confirmed against an actual live Teams or Zoom
call. Walk me through running `go run ./cmd/callmqtt --once` before, during, and after a real
call and checking the printed state/confidence/reasons make sense at each point. If it holds up,
update README.md to drop the "🚧 Status: early development... Not yet usable" line.
```

## ⬜ (unscoped) — macOS platform support
No owning subagent exists yet (`win-platform` only covers `platform/windows/`).
Worth revisiting once T36–T38 are done and Windows detection has proven itself
live, rather than starting it now.
