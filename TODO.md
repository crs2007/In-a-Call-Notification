# Review remediation plan

Ordered by dependency, then severity. Each item is meant to be one PR
(or one commit on a branch) with its own regression test. Items inside a
phase can be done in any order; phases should be done in order because
later ones build on the ownership changes in earlier ones.

Conventions used below:

- **Repro** — the test that must fail before the change and pass after.
  Put it next to the code it guards; keep it after the fix.
- **Done when** — the acceptance criteria. Don't tick the box until every
  line holds.
- **Touches** — files expected to change. If the fix wants to spill outside
  that list, stop and reconsider the approach.

---

## Phase 0 — Out-of-band, do today

### [x] 0.1 Rotate the leaked broker credentials

The password `mqcommunicator1!` for user `mqcommunicator` on
`192.168.68.166` is in `internal/config/example.yaml` at `4733015`, which
is on `origin/main`. Assume it is public.

- [x] Change that user's password on the Mosquitto broker (or delete the
      user and create a new one).
- [x] Update the real `%APPDATA%\callmqtt\config.yaml` on each machine
      to the new secret via `${CALLMQTT_MQTT_PASSWORD}`.
- [x] Decide whether to rewrite history. Recommendation: **don't** — the
      value is already rotated, and a force-push on `main` costs more than
      it saves. Note the rotation date in `SECURITY.md` instead.

**Done when:** the old password is rejected by the broker. **Confirmed
done by the user; history was not rewritten, per the recommendation
above.**

---

## Phase 1 — Connection and generation lifetimes

These three are one design problem: the supervisor never decided who owns
the broker connection's lifetime. Fix them together on one branch
(`fix/supervisor-lifetimes`), one commit each, so each has a clean test.

### [x] 1.1 Publisher lifetime must not derive from the caller's ctx

**Bug:** `tray.applyChange` builds a 15s ctx, `Supervisor.Reload` passes it
to `NewPublisher` → `autopaho.NewConnection(ctx)`. When the goroutine's
`defer cancel()` runs, autopaho tears down the connection and stops
reconnecting. Every tray toggle kills MQTT until restart.

**Touches:** `internal/supervisor/supervisor.go`, `internal/mqtt/client.go`,
`cmd/callmqtt/main.go`, `internal/supervisor/supervisor_test.go`.

- [x] Give `Supervisor` its own long-lived root ctx, created in `New`
      (`s.ctx, s.cancel = context.WithCancel(context.Background())`),
      cancelled in `Close`.
- [x] `build` passes a **per-generation** child of `s.ctx` to `newPub`,
      and stores that generation's `cancel` on the `generation` struct
      alongside the engine cancel. `stop` cancels it after `pub.Close`.
- [x] The caller's ctx to `Start`/`Reload` bounds only the *build*
      (detector construction, any `AwaitConnection` you add later), never
      the connection.
- [x] `mqtt.New` doc comment: state explicitly that `ctx` is the
      connection's lifetime.
- [x] **Repro:** in `supervisor_test.go`, make `fakePublisher` record the
      ctx it was built with and expose `ctxDone() bool`. Test: call
      `Reload` with a ctx you cancel immediately after it returns; assert
      the live generation's publisher ctx is **not** done. Add the mirror
      assertion that after `Close`, it **is** done (no leak).
      (`TestPublisherCtxOutlivesCallersCtx`)

**Done when:** on a real broker, toggle "Home Assistant discovery" in the
tray five times; `mosquitto_sub -t 'desktop-presence/#' -v` keeps showing
heartbeats afterwards and the tray says "Broker: connected". **Code and unit
tests done; this manual, real-broker check is still outstanding.**

### [x] 1.2 Reload must not run two clients with one ClientID, and must end with `online`

**Bug:** new generation starts before old stops. Same `client_id` → broker
session takeover fires the old Will (`offline`), then `old.Close` publishes
`offline` retained *after* the new client's `online`. Net: entity
unavailable after every settings change.

**Touches:** `internal/supervisor/supervisor.go`, `internal/mqtt/client.go`,
`internal/config/config.go` (only if you choose option B).

Pick one — A is recommended:

- **A. Serialize the swap around the broker.** In `Reload`: build the new
  engine+detectors (cheap, may fail), then `stop(old)` (publishes
  `offline`, disconnects), *then* construct the new publisher and start
  the new engine. Cost: a ~1–2s gap in publishing on every settings
  change, which is fine for a light. The "build before tear down"
  guarantee is preserved for everything that can actually fail at build
  time (bad detector rules, bad network CIDR); a broker that can't be
  reached isn't a build failure anyway — autopaho retries.
- **B. Per-generation ClientID suffix** (`callmqtt-<device>-<gen>`), and
  have the new client re-publish `online` after `old.Close` returns. More
  moving parts; only worth it if the publishing gap in A matters.

- [x] Implement A.
- [x] Extend `fakePublisher` with an ordered event log (`built`, `closed`)
      shared across instances; assert on Reload the order is
      `close(old)` → `build(new)`. (`TestReloadStopsOldBeforeBuildingNew`;
      also split the old build-failure test in two —
      `TestReloadLeavesRunningGenerationOnDetectorFailure` for a failure
      that still runs before `stop(old)`, and
      `TestReloadWithNoLiveGenerationOnPublisherFailure` for the accepted
      tradeoff where a — rare — publisher build failure now leaves no live
      generation, since old is already stopped by then.)
- [ ] **Repro (integration, manual):** with `mosquitto_sub -t
      'desktop-presence/+/availability' -v`, toggle a tray setting; the
      last retained value must be `online`.

**Done when:** the integration check passes and `TestReload*` covers the
ordering. **`TestReload*` coverage done; the manual, real-broker check is
still outstanding.**

### [ ] 1.3 SIGINT/SIGTERM must publish `offline` (and must actually exit the tray build)

**Bug:** the signal ctx is the first publisher's connection ctx. On signal
autopaho sends DISCONNECT reason 0 (deletes the Will) and nils its client
before `sup.Close` gets to publish `offline`. In the tray build the ctx is
ignored entirely, so the signal is swallowed.

**Touches:** `cmd/callmqtt/main.go`, `cmd/callmqtt/ui_tray.go`,
`cmd/callmqtt/ui_headless.go`.

- [x] After 1.1 the publisher no longer uses the signal ctx, so the
      clean-DISCONNECT race disappears. Verify by reading
      `mqtt.New` call sites — none should receive `ctx` from
      `signal.NotifyContext`. (Confirmed: `newPub`'s `pubCtx` now derives
      from `Supervisor.ctx`, not the caller's ctx.)
- [x] `ui_tray.go`: pass ctx through to `tray.Run` instead of discarding it.
      Implemented the watcher itself inside `tray.Run` (rather than
      wrapping it in a goroutine in `ui_tray.go` with a separate
      `tray.Quit`/`*app` handle): a small goroutine selects on `ctx.Done()`
      and calls `a.tray.Remove()` — the same call the "Quit" menu item
      makes — so the message loop exits and the shutdown path is shared.
- [x] Add `-H=windowsgui` caveat to the comment: SIGINT only arrives from
      a console; SIGTERM equivalent on Windows is `WM_CLOSE`/logoff — out
      of scope, but say so.
- [ ] **Repro (manual):** headless build, Ctrl-C while subscribed to the
      availability topic → retained `offline` appears within 1s. Tray
      build, `taskkill /pid <pid>` (no `/f`) → process exits.

**Done when:** both manual checks pass; `mqtt` package doc's "four guards"
paragraph is true again. **Code done; the two manual checks above are
still outstanding (need a console + a tray-build binary to run them).**

---

## Phase 2 — Config and secrets

### [x] 2.1 `Save` must never write an env-referenced password as a literal

**Bug:** `Config.Settings()` seeds `Password` from the expanded value;
`Save` rewrites `mqtt.password` unconditionally. Any checkbox toggle
writes the real secret into `config.yaml`.

**Touches:** `internal/config/save.go`, `internal/config/save_test.go`,
`internal/tray/tray.go`, `cmd/callmqtt/dialog_windows.go`.

- [x] Change `Settings.Password` semantics: add `PasswordChanged bool`.
      `Config.Settings()` sets `Password` to the expanded value (the
      dialog needs to pre-fill it) and `PasswordChanged=false`.
- [x] `Save`: write `mqtt.password` **only if** `PasswordChanged`.
      Otherwise leave the node untouched (which preserves `${VAR}`).
- [x] `dialog_windows.go` / `openBrokerDialog`: set `PasswordChanged =
      fields.Password != current.Password`.
- [ ] **Follow-up, not done here:** `Save` still unconditionally rewrites
      `host`, `port`, `username`, `discovery.enabled`, and every detector.
      Lower severity than the password (none of those are secrets), and
      fixing it properly means turning `Settings` into a patch
      (`*string`/`*int`/`*bool` fields, nil = untouched) with each tray
      action setting exactly one field — a bigger refactor touching every
      call site in `tray.go`. Scoped out of this PR; the shared-slice
      aliasing on `AllowedNetworks` this would also fix is still there.
- [x] **Repro:** `TestSaveLeavesEnvPasswordAloneWhenUnchanged` — config
      with `password: ${CALLMQTT_TEST_PASSWORD}`, env set, toggle one
      detector, `Save`, assert the file still contains the literal
      `${CALLMQTT_TEST_PASSWORD}` and not the secret.
- [x] Second test: `TestSaveWritesPasswordWhenChanged` — a dialog-driven
      change with `PasswordChanged=true` **does** write the new literal
      (and `PasswordIsLiteral()` becomes true on reload, so the warning
      fires).

**Done when:** both tests pass; a config that referenced env before a
tray click still references env after it. **Done** — `go vet ./...`,
`go test ./...`, and `go build/vet/test -tags tray ./...` all pass.

### [x] 2.2 Scrub the example config and fix the `init` message

**Touches:** `internal/config/example.yaml`, `cmd/callmqtt/main.go`,
`internal/config/config_test.go`.

- [x] `example.yaml`: `host: 192.168.1.10`, `username: callmqtt`,
      `password: ${CALLMQTT_MQTT_PASSWORD}`, `cidrs: ["192.168.1.0/24"]`.
      Kept the comments.
- [x] **Repro:** `TestExampleConfigHasNoWorkingCredentials` — parses
      `config.Example` with the env var unset, asserts
      `PasswordIsLiteral() == false` and `MQTT.Password == ""`.
- [x] `initConfig` output: the `CALLMQTT_MQTT_PASSWORD` sentence is now
      true (the file only ever contains the `${VAR}` reference), and it
      now tells the user how to set it (`setx` on Windows, plus the
      new-terminal caveat).
- [x] **Follow-up:** added a `check-example-config` job to
      `.github/workflows/ci.yml` (ubuntu-latest, no Go toolchain needed)
      with two grep-based steps: one fails if `mqtt.password` in
      `example.yaml` is anything but a `${VAR}` reference, the other fails
      if the rotated `mqcommunicator`/`192.168.68.*` strings ever reappear.
      Note: the TODO's suggested `grep -n 'password: [^$]'` false-positives
      on the current, correct file (`\s*` before `[^$]` can back off and
      match the space right before `${`); used
      `'^\s*password:\s*[^$[:space:]]'` instead, verified against the real
      file plus hand-built good/bad cases before landing.

**Done when:** `callmqtt init` + `--validate-config` with no env var set
reports only the "mqtt.host"-style problems you'd expect, not a working
config pointing at someone's broker. **Done** — `go vet`, `go build`, and
`go test` (with and without `-tags tray`) all pass; `grep -rn
'mqcommunicator|192.168.68' internal/config/example.yaml` is empty.

### [x] 2.3 Expand `${VAR}` after parsing, not before

**Bug:** textual substitution into the YAML source. A password containing
`#` is silently truncated; `: `, leading `*`/`&`/`[`, or newlines break
parsing or inject keys.

**Touches:** `internal/config/config.go`, `internal/config/config_test.go`.

- [x] Removed `expandEnv([]byte)`. `Parse` now unmarshals the raw file
      first, unexpanded.
- [x] Added `expandField(field, value string) string`, called explicitly
      for exactly the fields where env makes sense: `mqtt.host`,
      `mqtt.username`, `mqtt.password`, `mqtt.client_id`, `logging.file`,
      `rules_file`. No reflection over the whole struct.
- [x] `passwordFromEnv` is now `envPattern.MatchString(cfg.MQTT.Password)`
      read right after unmarshal, before `expandField` overwrites it.
      Deleted `referencesEnv` and its second parse.
- [x] Unset variable: still expands to empty (`Validate` catches it), but
      `expandField` now logs at `slog.Warn` with the field name and the
      variable that was missing.
- [x] **Repro:** `TestReproHashInPassword` (env `hunter2 #2024` round-trips
      intact) and `TestReproNewlineInPassword` (a newline in the env value
      can't inject a YAML key — asserts `logging.level` still comes from
      the file, not the injected value). Also added
      `TestUnsetEnvVarExpandsEmpty` for the unset-variable path, and
      updated `TestBareDollarIsNotExpanded` to call `expandField` directly
      now that `expandEnv([]byte)` is gone.

**Done when:** a password containing any YAML metacharacter survives
`Load` byte-for-byte. **Done** — `go vet ./...`, `go build ./...` (with
and without `-tags tray`), and `go test ./...` (with and without
`-tags tray`) all pass.

---

## Phase 3 — Engine correctness and data races

### [x] 3.1 A failed state-change publish must be retried on the next poll

**Bug:** `changed` is consumed even when `PublishState` fails; the new
state waits for the heartbeat (up to 60s).

**Touches:** `internal/engine/engine.go`, `internal/engine/engine_test.go`.

- [x] Add `pending bool` to `Engine`. Set it when `changed || rejoined ||
      startupAnnouncement`. Publish when `pending || heartbeatDue`. Clear
      `pending` only on publish success.
- [x] Log the failure at `Warn` with a "will retry" suffix and — since it
      now retries every 2s — rate-limit the log line (once per 30s, or
      log only on first failure and on recovery).
- [x] **Repro:** `TestReproLostTransitionOnTransientPublishFailure` from
      the review, renamed `TestTransientPublishFailureDoesNotLoseTransition`.

**Done when:** the test passes and `TestFailedPublishIsRetried` still
passes.

### [x] 3.2 `Engine.Status()` must be safe to call from another goroutine

**Touches:** `internal/engine/engine.go`.

- [x] Guard `e.status` with a `sync.RWMutex` (write under lock at the end
      of `evaluate`, `Status()` takes `RLock` and returns a copy). Copy
      the `Apps`/`Reasons` slices on write so the reader can't alias the
      resolver's buffers.
- [x] Add a `-race` test: run `Evaluate` in a loop on one goroutine and
      `Status()` on another for ~100ms. CI runs `-race` on Windows so
      this will actually be checked.

### [x] 3.3 `VisibleWindows` must be safe under concurrent callers

**Touches:** `platform/windows/windows.go`.

- [x] Wrap `VisibleWindows` in a package `sync.Mutex` — `EnumWindows` is
      synchronous per call, but the supervisor (even after 1.2) can have
      the old engine mid-poll when the new one starts. Update the comment
      to say why. Added `enumMu`, locked for the reset-call-copy sequence;
      the stale "unsynchronised buffer is safe here" comment is gone.
- [x] Alternative once 5.1 lands: the mutex is still correct and cheap;
      keep it.
- [x] Added `platform/windows/windows_test.go` (new,
      `//go:build windows`): `TestVisibleWindowsConcurrent` hammers
      `VisibleWindows()` from 10 goroutines × 50 calls.

**Done when:** `go test -race ./platform/windows/...` is clean. **Code and
test done; `-race` itself could not be run in this sandbox (no C
compiler, so `CGO_ENABLED` can't turn on) — needs confirming on the
Windows CI runner, which has the toolchain.**

### [x] 3.4 `tray.app.lastIcon` race

**Touches:** `internal/tray/tray.go`, `internal/tray/tray_test.go`,
`internal/supervisor/supervisor.go`.

- [x] Route every UI update through the refresh loop: `applyChange` and
      `togglePause` should send on a `refreshNow chan struct{}` (buffered
      1) instead of calling `a.refresh()` directly. `refreshLoop` selects
      on ticker + that channel. Then `lastIcon` is touched by exactly one
      goroutine. Implemented via a `requestRefresh()` helper doing a
      non-blocking send.
- [x] `togglePause`: after `SetPaused`, the engine's `status.Paused` is
      stale until its next tick. Chose the "derive from `Paused()`"
      option: added `Supervisor.Paused()` (reads the engine's atomic
      flag, not the poll snapshot); `togglePause` and `refresh()` both
      read it directly instead of `Status().Paused`.
      `Options.Supervisor` is now a `supervisorAPI` interface (new seam,
      mirrors `engine.Publisher`) so `tray_test.go` can fake it.
- [x] Added `TestRefreshIsRaceFreeUnderConcurrentApplyChangeAndTogglePause`
      in `tray_test.go`, with a `fakeSupervisor`.

**Done when:** `go test -race -tags tray ./internal/tray/` passes with a
test that hammers `refresh` and `applyChange` concurrently (fake
supervisor is fine; `tray_test.go` already has scaffolding). **Code and
test done; same `-race`-in-this-sandbox limitation as 3.2/3.3 — needs
confirming on Windows CI.**

---

## Phase 4 — Controls that don't do anything

### [x] 4.1 Honour `detectors.<app>.enabled`

**Touches:** `cmd/callmqtt/main.go`, `internal/detectors/detectors.go`,
`internal/detectors/detectors_test.go`, `internal/config/config.go`,
`internal/config/config_test.go`.

- [x] `detectors.New` takes `enabled func(app string) bool` and skips
      rules whose app is disabled. Unknown app names in config (typo:
      `team:`) are now reported by `Validate` — checked against
      `rules.Default()`'s app list, sorted for deterministic output.
- [x] `buildDetectors` passes a closure over `cfg.Detectors`.
- [x] Test: `TestNew_DisabledAppSkipped` — rules with teams+zoom+slack,
      config disables zoom, `New` returns two detectors and neither is
      zoom. Validate side: a `detectors.team` typo test asserting the
      error names the bad key.

**Done when:** unticking "Zoom" in the tray and running `--once` with
Zoom in a meeting reports `inactive`. **Code and unit tests done; this
manual, real-app check is still outstanding.**

### [x] 4.2 Decide `rules_file` and `inactive_threshold`

Both are parsed, validated, documented, and never read. Pick per field:

- [x] `rules_file`: **implement** — it's the natural escape hatch when a
      Teams update changes a title and the user can't wait for a release.
      `buildDetectors`: if set, `rules.LoadFile(path)`, else `Default()`.
      Resolve relative to the config file's directory. Added to
      `example.yaml` commented out.
- [x] `inactive_threshold`: **implement hysteresis or delete it.**
      Implemented in `detectors.Detector`, not `rules.Evaluate` —
      `rules.CompiledRule.Evaluate`/`Config.Evaluate` are deliberately
      stateless and fixture-tested by ~15 call sites in `rules_test.go`,
      so the hysteresis memory (`wasActive bool`) lives on the long-lived
      `Detector` instance instead, which already wraps one `Evaluate` call
      per poll. Once active, stays active until confidence drops *below*
      `inactive_threshold`, not merely below `active_threshold`. This
      directly addresses the Teams "generic title without mic" flicker
      described in `rules.yaml`.
- [x] **Repro:** `TestDetect_HysteresisHoldsActiveBetweenThresholds` —
      confidence walks 0.85 → 0.60 → 0.25 (the 0.60 step isolated via a
      mic-only match, since a window match in this rule engine always
      implies a process match too, making a window-only 0.60 unreachable);
      state stays Active through 0.60 and only drops to Inactive at 0.25.

### [x] 4.3 `Validate` must reject an empty `device_id`

**Touches:** `internal/config/config.go`.

- [x] After `applyDerivedDefaults`, if `App.DeviceID == ""` add a problem
      ("device_id resolves to empty; set app.device_id explicitly").
      Non-ASCII hostnames hit this. **Repro:** a `device_id` that
      slugifies to empty (`"###"`) fails `Validate` with that message.

---

## Phase 5 — Performance

### [ ] 5.1 Observe the platform once per tick, not once per detector

**Touches:** `internal/detectors/detectors.go`, `internal/engine/engine.go`
(only if you choose B).

- **A (minimal):** a `sharedSnapshot` wrapper that caches the
  `rules.Observation` for the duration of one `Detect` sweep. Key it on a
  tick counter the engine bumps, or simply memoize for ≥1s
  (`if time.Since(last) < 500ms { return cached }`). Detectors still
  implement `model.Detector` unchanged.
- **B (cleaner):** one `Detector` that owns all rules and returns
  `[]DetectionResult`; `model.Detector` becomes `Detect(ctx)
  []DetectionResult`. Touches more but removes the fake independence.

- [ ] Implement A now; note B as a follow-up.
- [ ] Test: fake `Snapshot` funcs count invocations; three detectors,
      one poll → each platform func called exactly once.

### [ ] 5.2 Replace gopsutil in `ProcessNames` with one Toolhelp32 snapshot

**Touches:** `platform/windows/process.go`, `go.mod` (drop gopsutil if
`cmd/probe` doesn't need it either).

- [ ] `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` +
      `Process32First/Next` from `golang.org/x/sys/windows`; ~25 lines.
      Names come from `ProcessEntry32.ExeFile`. No `OpenProcess` at all.
- [ ] Run `go run ./cmd/probe --count 1` before/after; the output must be
      identical.

### [ ] 5.3 Bound every publish

**Touches:** `internal/mqtt/client.go`.

- [ ] In `Client.publish`, wrap ctx with `context.WithTimeout(ctx, 5s)`
      (constant, documented). The engine's poll loop can then never stall
      more than one tick-plus-5s on a half-open socket.
- [ ] Consider `KeepAlive: 20` → also set `paho.ClientConfig`'s
      `PacketTimeout` so a stuck PUBACK is detected independently.

### [ ] 5.4 Log file growth

**Touches:** `cmd/callmqtt/main.go`.

- [ ] Simplest adequate: on open, if the file is > 5 MB, rename to
      `callmqtt.log.1` (overwrite) and start fresh. No dependency, one
      generation of history. Document in `example.yaml`.
- [ ] Combined with 3.1's rate-limited retry log, the broker-down case no
      longer writes one line per 2s.

---

## Phase 6 — The network gate is weaker than it claims

### [ ] 6.1 Reject rules that can never match

**Touches:** `internal/config/config.go`, `internal/network/local.go`.

- [ ] Expose from `network` what the current checker can populate
      (`LocalChecker{}.Capabilities()` → `{CIDR: true, SSID: false, …}`).
- [ ] `Validate` (or `engine.New`, which already compiles the matcher)
      reports a rule whose *only* matchers are unsupported ("rule Home
      matches on ssids only; SSID detection is not implemented on this
      platform, add a cidrs entry"). A rule with `ssids` **and** `cidrs`
      is fine.

### [ ] 6.2 Stop treating virtual adapters as evidence

**Touches:** `internal/network/local.go`, `internal/network/local_test.go`.

- [ ] Skip interfaces whose name matches the known virtual set on
      Windows (`vEthernet`, `VirtualBox Host-Only`, `VMware`, `Hyper-V`,
      `WSL`, `Loopback`, `Bluetooth`) **and** interfaces without
      `FlagRunning`. Keep it a denylist with a comment; it's heuristics,
      say so.
- [ ] Better: Windows `GetAdaptersAddresses` gives `IfType` and
      `OperStatus`; filter to `IF_TYPE_ETHERNET_CSMACD` /
      `IF_TYPE_IEEE80211` with `IfOperStatusUp`. That's a real
      `platform/windows/network.go` and the `win-platform` agent's job.
      Do the denylist now, file the proper version as a follow-up.

### [ ] 6.3 Document the honest threat model

**Touches:** `README.md`, `SECURITY.md` (via the `release-ci` agent).

- [ ] State plainly: matching is by local subnet only; `192.168.1.0/24`
      is not a unique identity; recommend gateway MAC/BSSID once
      implemented and, until then, a non-default home subnet.
- [ ] Remove or mark "planned" every mention of SSID/BSSID matching in
      the design doc that reads as shipped.

---

## Phase 7 — Smaller smells (batch into one "cleanup" PR)

- [ ] **7.1** `onConnectionUp` comment says it republishes state; make it
      true — add a `SetStateSource(func() (Payload, bool))` the engine
      wires, and publish it after `online`. Then `retain: false` becomes
      a viable setting.
- [ ] **7.2** `platform/windows/dialog.go:182` — hoist
      `syscall.NewCallback(dialogWndProc)` to a package `var` like
      `windows.go` does. Use the `err` return of `.Call` instead of a
      separate `windows.GetLastError()` at :189 and :211.
- [ ] **7.3** `tray.openFile` — `windows.ShellExecute(0, "open", path,
      "", "", SW_SHOWNORMAL)` on Windows instead of `cmd /c start`.
- [ ] **7.4** ConsentStore liveness: in `devicesInUse`, drop entries whose
      owning process (packaged → package family; NonPackaged → exe path)
      is not in the current process list. Requires 5.1's shared snapshot
      so it's free. Add a fixture: Teams mic entry with `Stop=0` but no
      `ms-teams.exe` running → not in use.
- [ ] **7.5** TLS: add `mqtt.tls.ca_file` (PEM) so self-signed brokers
      don't need `insecure_skip_verify`. Optional `cert_file`/`key_file`.
- [ ] **7.6** `expire_after` guard: `Validate` should fail if
      `heartbeat_seconds * 1.5 < detect_seconds * 2` — not possible with
      current minimums, but the invariant lives in two packages and
      nothing ties them.
- [ ] **7.7** `slugify`: trailing-hyphen handling walks `b.String()` on
      every non-alnum rune (quadratic on pathological input). Track
      `lastWasSep bool` instead.
- [ ] **7.8** `supervisor.stop` 5s wait + `Close` 5s shutdown ctx +
      publish → the worst-case quit is ~15s of "why is it still in the
      tray". Cap total shutdown at 5s by sharing one deadline.

---

## Verification checklist for the whole series

Run before tagging the next release:

- [ ] `go vet ./... && go test -race -count=1 ./...` on Windows with
      `CGO_ENABLED=1` (CI does this; do it locally too once).
- [ ] Manual, against a real Mosquitto with `mosquitto_sub -v -t '#'`:
  - [ ] start → `online`, discovery, `unknown`, then `inactive`
  - [ ] join a Teams call → `active` within `enter_debounce + 2s`
  - [ ] toggle every tray checkbox once → availability stays `online`,
        heartbeats continue
  - [ ] `--simulate` for two full cycles → `active`/`inactive` alternate
  - [ ] Quit from tray → `offline` retained
  - [ ] Ctrl-C (headless) → `offline` retained
  - [ ] kill broker for 90s mid-call, restart → `online` + state
        reasserted within one heartbeat
- [ ] `grep -rn 'mqcommunicator' .` returns nothing.
- [ ] README Installation section still matches `.goreleaser.yaml`
      (nothing here changes the release shape, so no edit expected).
