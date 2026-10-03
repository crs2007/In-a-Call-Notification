# macOS Support Plan — In a Call Notification (`callmqtt`)

## Context

`callmqtt` is a Go desktop agent that infers "user is in a Zoom / Teams / Slack
call" from weak local signals (processes, window titles, microphone holder),
debounces it, and publishes a retained MQTT state + Home Assistant discovery so
a "do not disturb" light follows the call. Today it ships **Windows-only**
(`.goreleaser.yaml`: `goos: [windows]`, amd64 + arm64, tray build, CGO off).

The codebase was built with macOS in mind but never delivered it:
- `platform/windows/stub_other.go` returns "no evidence" for every signal on
  darwin, so a darwin build compiles (CI job `cross-compile-darwin`) but can
  **never** report `active`.
- `cmd/callmqtt/startup_stub.go` / `dialog_stub.go` stub out login-item and
  the broker dialog.
- The vendored tray (`third_party/systray`, purego/goffi, no cgo) already has
  an NSStatusBar backend incl. `SetTemplateIcon`.
- `docs/In a call notification.md` §9–10, §14, §25–26, §39 sketch the mac
  design (Accessibility, CoreWLAN, SMAppService, universal DMG).

Goal: make macOS a first-class, released, supported platform with the same
user-facing contract (same MQTT payload / HA entity, same "false positive is
the worst failure" bar) as Windows.

---

## 1. Scope Definition — what "full support for Mac" means

**In scope (v1 = "GA on macOS")**
- **Native menu-bar app**, not a compatibility layer: a signed, notarized
  `CallMQTT.app` (LSUIElement — menu bar only, no Dock icon), universal binary
  (arm64 + amd64), pure Go, `CGO_ENABLED=0` kept (purego/goffi FFI, same
  approach as the vendored systray).
- **Detection parity** for Teams (new Teams, `com.microsoft.teams2`), Zoom
  (`us.zoom.xos`) and Slack huddles (`com.tinyspeck.slackmacgap`), using the
  same three-signal model (process + window title + mic holder) and the same
  confidence/debounce engine, with macOS-specific rules backed by captured
  fixtures.
- **Feature parity in the tray**: state/network/broker display, pause, allow
  current network, broker settings dialog, start-at-login, open config/log —
  plus a mac-only **Permissions** section (Accessibility status + "Open
  Privacy Settings").
- **Network gating parity**: CIDR matching (what Windows ships today) with a
  macOS virtual-interface classifier.
- **Release parity**: every push to `main` also publishes a macOS asset;
  README Installation, SECURITY and docs updated.
- **Graceful degradation**: any missing permission degrades detection, never
  crashes or produces `active` on less evidence.

**Acceptance / non-functional targets**
| Metric | Target |
| --- | --- |
| Supported OS | macOS 13 Ventura+ (full mic attribution on 14 Sonoma+) |
| Hardware | Apple Silicon and Intel |
| False-positive rate | 0 in the scenario matrix (music, idle apps open, mute/unmute, personal call apps) |
| Detection latency | call start → `active` ≤ enter-debounce + 1 poll (same as Windows); call end → `inactive` ≤ exit debounce (~8 s) |
| Idle CPU | < 1 % of one core averaged at default poll; RSS < 40 MB |
| Poll cost | one platform observation per tick (existing `sharedObserver`) |
| Live confirmation | each app confirmed against a real call on mac (as T38 did for Teams/Windows) |

**Out of scope for v1 (explicit non-goals)**
- SSID/BSSID/gateway matching (needs Location permission on macOS 14+, and is
  unimplemented on Windows too — keep parity at CIDR-only).
- Accessibility-tree *content* inspection beyond window titles (e.g. Slack
  huddle UI elements) — Phase 5 stretch.
- Mac App Store distribution (sandbox forbids reading other apps' windows).
- Linux.
- Teams log / power-assertion heuristics (design doc §9) — candidate Phase 5
  signals if fixtures show titles are insufficient.

**Assumptions (stated explicitly — please correct if wrong)**
1. Team: 1 senior Go engineer (owner) + part-time QA/reviewer; Claude subagents
   (`go-core`, `release-ci`, `detector-rules`, and a new `mac-platform`) do
   scoped work. Estimates are in engineer-weeks for that shape.
2. Someone has (or will buy) an **Apple Developer Program** membership
   ($99/yr) for a Developer ID certificate + notarization; secrets can be added
   to GitHub Actions.
3. At least one physical Mac (ideally one Apple Silicon + one Intel) is
   available for fixture capture and live confirmation; GitHub `macos-latest`
   runners are acceptable for CI.
4. OSS GoReleaser (no Pro). `.app` bundling and DMG creation are therefore done
   by a small script/step rather than GoReleaser Pro's `app_bundles`/`dmg`.
5. Minimum OS 13 is acceptable; 12 and older unsupported.

---

## 2. Gap Analysis

| Area | Windows today | macOS today | Gap / macOS mechanism |
| --- | --- | --- | --- |
| Process list | `CreateToolhelp32Snapshot` (`platform/windows/process.go`) | stub → nil | `sysctl kern.proc.all` via `golang.org/x/sys/unix` (pure Go). `p_comm` is truncated to 16 chars → resolve full name with `proc_pidpath` (libproc via purego) for matching. |
| Window titles | `EnumWindows` + `GetWindowTextW` (`platform/windows/windows.go`) | stub → nil | **Biggest gap.** `CGWindowListCopyWindowInfo` returns owner PID/name freely, but **titles require Screen Recording** (and Sequoia re-prompts periodically). Recommended: **Accessibility API** (`AXUIElementCreateApplication` → `kAXWindowsAttribute` → `kAXTitleAttribute`) only for PIDs of rule-listed apps; requires Accessibility (TCC) grant. CGWindowList used only for "on-screen window exists" without titles. |
| Mic in use, attributed | Registry ConsentStore (`consent.go`) → exe path / package family | stub → nil | CoreAudio process objects (`kAudioHardwarePropertyProcessObjectList`, `kAudioProcessPropertyIsRunningInput`, `…BundleID`, `…PID`) on macOS 14+ — no TCC prompt. macOS 13 fallback: device-level `kAudioDevicePropertyDeviceIsRunningSomewhere` (unattributed → treated as *no* app evidence, to keep the false-positive bar). Audio may be held by a helper process (e.g. Teams/Zoom helpers) → map helper bundle IDs via fixtures. |
| Camera in use | ConsentStore webcam | stub → nil | CoreMediaIO `kCMIODevicePropertyDeviceIsRunningSomewhere` — device-level only. Not used by current rule weights; implement as unattributed, low priority. |
| Detection rules | `internal/rules/rules.yaml` keyed on `ms-teams.exe`, `Zoom.exe`, `Slack.exe`, Windows titles/regexes | n/a | Process names, window titles and mic identifiers all differ (e.g. `zoom.us`, `Microsoft Teams`, `Slack`; bundle IDs instead of exe paths). Rules need an OS dimension + new fixtures in `testdata/probe/darwin/`. |
| Platform abstraction | `internal/detectors` imports `platform/windows` directly; `Snapshot` already injects funcs | — | Types (`WindowInfo`) live in a package literally named `windows`; `detectors.WindowsSnapshot()` and `internal/probe` hard-wire it. Need a neutral seam. |
| Network | `LocalChecker` (stdlib) + `adapters_windows.go` IfType/OperStatus filter | stdlib works; `adapters_other.go` returns nil; name denylist is Windows-flavoured | Add `adapters_darwin.go`: allow `en*` (Wi-Fi/Ethernet/USB-Ethernet) with `FlagUp|FlagRunning`; reject `utun*`, `awdl*`, `llw*`, `bridge*`, `anpi*`, `ap*`, `gif*`, `stf*`, `vmenet*`, `lo*`. |
| Tray | gogpu/systray Win32 backend, coloured icons | darwin backend exists (purego) | Must run on main thread (`runtime.LockOSThread` in `main.init`), use **template icons** for menu-bar dark/light, verify menu checkboxes/callbacks on darwin. Add Permissions submenu. |
| Broker dialog | native Win32 dialog (`platform/windows/dialog.go`, 376 LOC) | stub → open config file | `osascript` `display dialog` sequence (host, port, user, hidden password) — no cgo, small. |
| Error reporting | `ShowError` MessageBox | stub (no-op) | `.app` has no console → `osascript display alert`. |
| Start at login | HKCU Run key (`startup.go`, pure part in `startup_pure.go`) | stub returns error | User **LaunchAgent** plist in `~/Library/LaunchAgents/com.crs2007.callmqtt.plist` + `launchctl bootstrap/bootout gui/$UID`. Pure plist rendering unit-testable. (SMAppService later; needs ObjC + bundle.) |
| Config / logs paths | `os.UserConfigDir()` → `%APPDATA%\callmqtt` | Works → `~/Library/Application Support/callmqtt` | Minor: docs; optionally logs to `~/Library/Logs/callmqtt`. |
| Open file | `ShellExecute` | `open` via `exec` (`internal/tray/tray.go:499`) | Already done. |
| Packaging | goreleaser zip, `-H=windowsgui` | none | `.app` bundle (Info.plist: `LSUIElement`, `NSAccessibilityUsageDescription`-style copy, bundle ID), universal binary, hardened runtime, Developer ID codesign, notarize + staple, zip (+ optional DMG, Homebrew cask). TCC grants bind to code signature → **unsigned builds lose permissions on every update**. |
| CI | `test-windows`, `cross-compile-darwin` (build only) | no tests run on mac | Add `test-macos` (`macos-latest`): vet, `go test -race`, tray build; release `verify` gains darwin builds; release job gains sign/notarize. |
| Diagnostics | `--probe`, `cmd/probe`, `testdata/probe/*.txt` | prints nothing useful | Probe must print mac signals + permission status so fixtures can be captured. |
| Docs | README Windows-centric (badges, "Windows PC", install) | — | README Installation/Quick Start/FAQ for mac (Gatekeeper, permissions), SECURITY threat-model additions (Accessibility grant scope). Must pass `readme-standards` validator. |

---

## 3. Architectural Changes

1. **Neutral platform seam** (keep the existing dependency rule: only
   `internal/detectors` and `internal/probe` touch platform code).
   - New `platform/signals` (types only): `WindowInfo{PID, Title}`,
     `Permissions{Accessibility, ScreenRecording, …}`. `platform/windows`
     re-exports via type alias so Windows code is untouched in behaviour.
   - New `platform/darwin` package (`//go:build darwin`) exposing the same
     free-function set: `ProcessNames`, `VisibleWindows`,
     `AppsUsingMicrophone`, `AppsUsingWebcam`, `StartupEnabled/Enable/Disable/Status`,
     `ShowError`, `ShowBrokerDialog`, `PermissionStatus`, `OpenPrivacySettings`.
     Each split into `*_pure.go` (logic, runs in Linux CI) + thin FFI file —
     mirrors `consent_pure.go` / `startup_pure.go`.
   - `internal/detectors`: replace `WindowsSnapshot()` with
     `PlatformSnapshot()` defined in `snapshot_windows.go` /
     `snapshot_darwin.go` / `snapshot_other.go`. `Snapshot` struct already the
     injection point — no engine changes.
   - Move the non-Windows stub out of `platform/windows/stub_other.go` into
     `platform/stub` (or `snapshot_other.go`) so `platform/windows` becomes
     `//go:build windows` only.
2. **OS-aware rules.** Add optional `goos:` to each rule in `rules.yaml`
   (default `windows` for existing entries to keep behaviour); `rules.Load`
   filters by `runtime.GOOS` (injectable for tests). Separate entries per
   (app, OS) because titles, weights and hazards differ per platform. Rule
   validation: at most one rule per (app, goos). Mic regexes on mac match
   **bundle IDs**.
3. **Capability/permission model.** Extend the network `Capabilities` idea to
   detection: platform reports which signals are available (e.g. titles
   unavailable without Accessibility). Engine/tray surface "degraded: grant
   Accessibility" instead of silently never going active. Rule scoring is
   unchanged — missing signals simply contribute 0 (already the contract in
   `model.Detector`).
4. **FFI strategy:** purego-style dynamic calls (reuse `github.com/go-webgpu/goffi`
   already in `go.sum` via systray, or `ebitengine/purego`) against
   `ApplicationServices`, `CoreAudio`, `CoreFoundation`, `libproc`. Keeps
   `CGO_ENABLED=0`, Linux-hosted cross-compilation and the single toolchain.
   Fallback if FFI proves brittle: a tiny cgo `darwin` build with a macOS
   runner (risk R3).
5. **Packaging layer:** new `packaging/macos/` (Info.plist template,
   entitlements, `bundle.sh`) consumed by the release workflow.
6. **New subagent** `.claude/agents/mac-platform.md` owning `platform/darwin/`,
   mirroring `win-platform`.

---

## 4. Code Changes (high level)

| Area | Files (new / changed) |
| --- | --- |
| Neutral types + stub | new `platform/signals/`; change `platform/windows/windows.go` (alias), move `platform/windows/stub_other.go` |
| macOS signals | new `platform/darwin/{process,process_pure,windows_ax,audio,camera,permissions,ffi}.go` + `_test.go` for pure parts |
| macOS OS integration | new `platform/darwin/{startup,startup_pure,dialog,alert}.go`; new `cmd/callmqtt/{startup_darwin,dialog_darwin}.go`; narrow `startup_stub.go`/`dialog_stub.go` to `!windows && !darwin` |
| Detectors / probe | `internal/detectors/detectors.go` (`PlatformSnapshot`), new `snapshot_{windows,darwin,other}.go`; `internal/probe/probe.go` (print permissions) |
| Rules | `internal/rules/rules.go` (`goos` field + filter + validation), `internal/rules/rules.yaml` (darwin entries), `internal/rules/rules_test.go` (fixture tests), new `testdata/probe/darwin/*.txt` |
| Network | new `internal/network/adapters_darwin.go` (+ pure classifier test); `adapters_other.go` → `!windows && !darwin` |
| Tray | `internal/tray/tray.go`: template icon on darwin (keep colour encoded as distinct glyphs, since template images are monochrome), Permissions submenu, `Options.Permissions` injection; `cmd/callmqtt/main.go`: main-thread lock on darwin |
| Build/release | `.goreleaser.yaml`: second build id `callmqtt-darwin` (`goos: darwin`, amd64+arm64, no `-H=windowsgui`), `universal_binaries`, separate archive; `.github/workflows/ci.yml` (`test-macos`), `release.yml` (darwin verify builds, macOS sign/notarize/staple job) |
| Packaging | new `packaging/macos/{Info.plist.tmpl,entitlements.plist,bundle.sh}` |
| Docs | README (Installation, Quick Start, permissions FAQ, badge), SECURITY (Accessibility scope), `docs/TASKS.md` task entries, CLAUDE.md release checklist (new assets) — via `release-ci` subagent, validated with `go run ./.claude/skills/readme-standards/scripts/validate_readme.go` |

Third-party compatibility: `paho.golang`, `yaml.v3`, `x/sys/unix` — pure Go, fine.
`gogpu/systray` darwin backend exists but is unproven in this project → smoke
test early (Phase 0). No new cgo dependencies.

---

## 5. Implementation Roadmap

Total ≈ **10–12 engineer-weeks** elapsed for one engineer, ~7–8 with a second
engineer on packaging/CI in parallel. Each phase ends in a releasable state
(mac assets can stay out of the release until Phase 4 via `[skip release]`-free
but darwin-excluded goreleaser config).

| Phase | Duration | Deliverables | Exit criteria / milestone |
| --- | --- | --- | --- |
| **0. Spikes & de-risking** | 1 wk | (a) systray darwin smoke test (menu, checkbox, template icon, main thread); (b) purego AX title read for Teams/Zoom/Slack; (c) CoreAudio process-object mic attribution; (d) signed-vs-unsigned TCC behaviour | Go/no-go on pure-Go FFI vs cgo; signal availability table per app recorded in `docs/` |
| **1. Platform seam refactor** | 1 wk | `platform/signals`, `PlatformSnapshot`, stub move, `goos` rule field (Windows behaviour byte-identical) | All Windows tests green; darwin cross-compile green; no rule/score change on Windows fixtures |
| **2. macOS signal adapters + probe** | 2–3 wk | `platform/darwin` process/window/mic/camera/permissions; probe output; `adapters_darwin.go` | `callmqtt --probe` on a real Mac lists processes, titles (with AX grant), mic bundle IDs; pure-part unit tests in Linux CI |
| **3. Rules & fixtures** | 1.5–2 wk | Capture `testdata/probe/darwin/` (idle, music, each app open-no-call, in-call, muted, huddle); darwin rules in `rules.yaml`; fixture tests | Scenario matrix passes in tests; **live confirmation** for Teams first, then Zoom, Slack (M1: "detects calls on Mac") |
| **4. OS integration & UX** | 1.5 wk | LaunchAgent startup, osascript broker dialog + error alert, tray Permissions submenu, template icons | Feature parity checklist vs Windows tray complete (M2: "feature parity") |
| **5. Packaging, CI, release** | 1.5–2 wk (parallelisable from Phase 2) | `.app` bundle, universal binary, codesign + hardened runtime, notarize + staple, `test-macos` CI job, release workflow, Homebrew cask (optional) | A tagged release publishes `callmqtt_<v>_darwin_universal.zip` that opens on a clean Mac without Gatekeeper warnings (M3: "shippable") |
| **6. Beta → GA** | 2 wk | `-beta.N` pre-release tags, dogfood on ≥3 Macs (Intel + Apple Silicon, macOS 13/14/15), perf measurement, README/SECURITY | 2 weeks of dogfooding with zero false positives; perf targets met; docs validated (M4: GA) |
| **7. Stretch** | later | AX-tree Slack huddle signal, SSID via CoreWLAN (Location), SMAppService login item, Teams power-assertion signal | Backlog |

**Resources**
- 1 Go engineer (owner), ~0.5 engineer for CI/packaging in Phase 5.
- Apple Developer Program account; GitHub secrets: Developer ID `.p12` + password, App Store Connect API key (notarytool).
- Hardware: ≥1 Apple Silicon Mac + ≥1 Intel Mac (or a cloud Mac), test accounts on Teams/Zoom/Slack with a second participant for live calls.
- CI: `macos-latest` minutes (~10× Linux cost) — limit to one mac job per CI run.

---

## 6. Risks and Mitigations

| # | Risk | Likelihood / Impact | Mitigation |
| --- | --- | --- | --- |
| R1 | Window titles unavailable without Accessibility; users decline the prompt | High / High | Rules designed so mic + process can still reach `active` only where fixtures prove it's safe; otherwise stay inactive (under-trigger). Tray shows "Detection degraded — grant Accessibility". Clear onboarding copy. Never request Screen Recording. |
| R2 | TCC grants reset on every update if signature changes (unsigned/ad-hoc builds) | High / High | Ship only Developer-ID-signed builds with a stable bundle ID and designated requirement; document `tccutil reset` for recovery. |
| R3 | purego FFI to CoreAudio/AX is brittle (struct layouts, CFRelease leaks, arm64 vs amd64 ABI) | Med / Med | Phase-0 spike decides; keep FFI in tiny files with pure logic separated; leak test by running probe 10k iterations; fallback to cgo + macOS runner build. |
| R4 | Mic held by helper process / different bundle ID than the app (Teams, Zoom helpers) | Med / High | Fixture capture per app per version; rules match helper bundle IDs explicitly; regression fixtures. |
| R5 | macOS 13 lacks per-process audio attribution | Med / Med | Unattributed mic is *not* counted as app evidence; document reduced accuracy on 13; consider raising floor to 14 if beta data shows misses. |
| R6 | Vendored systray darwin backend bugs (main-thread, menu updates from goroutines) | Med / High | Phase-0 smoke test; dispatch UI updates to main thread; upstream fixes as with the existing Windows patch in `third_party/systray/README.md`. |
| R7 | Apple OS updates change TCC / AX behaviour (e.g. Sequoia prompts) | Med / Med | CI on `macos-latest`, beta-channel dogfooding, probe tool as first-line diagnostic. |
| R8 | App vendors change titles/process names | High (ongoing) / Med | Same fixture-first discipline as Windows (`detector-rules` agent); probe output makes new fixtures cheap. |
| R9 | Notarization/signing secrets in CI leak or expire | Low / High | Store in GitHub environment with required reviewers; use App Store Connect API key (rotatable); cert expiry reminder. |
| R10 | Release pipeline coupling: every push to `main` releases — a broken mac step blocks Windows releases | Med / Med | Mac build/sign in `verify` before tagging; mac publish as a separate job so a notarization outage can be re-run without re-tagging (existing "tagged HEAD without release" recovery). |
| R11 | Privacy perception: Accessibility grant is powerful | Med / Med | Read titles only for rule-listed bundle IDs; never log titles above Debug (existing rule); document in SECURITY.md. |
| R12 | Network false-allow on mac (VPN `utun`, AWDL, Parallels/UTM bridges) | Med / Med | Darwin interface classifier + tests; keep `CheckCapabilities` rejecting unsupported fields. |
| R13 | CI cost / flakiness of mac runners | Med / Low | One mac job; most logic in pure files tested on Linux. |

---

## 7. Verification (how we prove it's done)

- **Unit (Linux + mac CI):** `go vet ./...`, `go test -race ./...` on
  windows-latest and macos-latest; pure tests for plist rendering, interface
  classifier, AX/CoreAudio result parsing, `goos` rule filtering; darwin
  fixture tests in `internal/rules/rules_test.go`.
- **Cross-build:** `GOOS=darwin GOARCH={arm64,amd64} go build -tags tray ./...`
  and Windows builds unchanged.
- **Windows regression:** existing Windows fixtures produce identical scores
  before/after Phase 1.
- **Manual scenario matrix on Mac** (Intel + Apple Silicon, macOS 13/14/15):
  idle; music playing; each app open with no call; each app in call; mute/
  unmute; Slack huddle; Accessibility denied → degraded but stable; sleep/wake
  mid-call; network change / VPN up; start-at-login after reboot; update
  preserves TCC grant. Observe with
  `mosquitto_sub -t 'desktop-presence/#' -v` and the HA entity.
- **Release:** downloaded zip on a clean Mac opens without Gatekeeper warning
  (`spctl -a -vv`, `stapler validate`); assets match README Installation;
  README validator passes.
- **Perf:** Activity Monitor / `ps` CPU & RSS over a 1-hour idle run.
