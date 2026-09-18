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
      per poll. Once active, stays active while confidence is at or
      above `inactive_threshold` *and* the app still holds the mic or
      webcam (`DetectionResult.Signals.DeviceHeld()`). The device
      condition was added for issue #2: every shipped rule's idle score
      (Teams/Slack process + window = 0.60) sits above the floor, so a
      confidence-only hold kept Teams `active` forever once the call
      ended with any non-Chat tab open. The mid-call dip the hold was
      built for (Meet tab switched away, mic held = 0.50) still holds.
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

### [x] 5.1 Observe the platform once per tick, not once per detector

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

- [x] Implemented A, contained entirely to `internal/detectors/detectors.go`
      (`engine.go` untouched). `New` builds one `*sharedObserver` (500ms TTL,
      keyed on wall-clock time via the injected `now`, guarded by a
      `sync.Mutex` in the same defensive style as `platform/windows`'s
      `enumMu`) and hands the same pointer to every `*Detector` it builds.
      `Detect` calls `d.shared.observe(d.snapshot)` instead of
      `d.snapshot.observe()` directly; `model.Detector` and `Snapshot` are
      both unchanged. B noted above as the not-taken cleaner alternative.
- [x] `TestDetect_HysteresisHoldsActiveBetweenThresholds` swaps one
      detector's `.snapshot` field three times against a clock that used to
      be frozen (`fixedNow(time.Now())` captured once); with caching keyed
      on elapsed wall-clock time, a frozen clock made steps 2 and 3 replay
      step 1's cached observation. Fixed by changing that test's injected
      clock to advance 1s (> the 500ms TTL) before each step, matching how
      the real engine's `time.Now()` naturally advances between polls in
      `buildDetectors` — each step is now correctly treated as a new poll
      sweep rather than a cache hit.
- [x] **Test:** `TestNew_SharesOneObservationAcrossDetectorsInOnePoll` —
      three detectors (teams/zoom/slack) from one `New()` call, all four
      `Snapshot` funcs wired to atomic counters; one poll sweep (`Detect` on
      each) asserts every counter is exactly 1, not 3.

**Done when:** `go test ./internal/detectors/... ./internal/engine/...`
passes. **Done** — `go vet` and `go test` both clean. `-race` could not be
run in this sandbox (`CGO_ENABLED=0`, no C compiler); needs confirming on
Windows CI.

### [x] 5.2 Replace gopsutil in `ProcessNames` with one Toolhelp32 snapshot

**Touches:** `platform/windows/process.go`, `go.mod` (drop gopsutil if
`cmd/probe` doesn't need it either).

- [x] `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` +
      `Process32First/Next` from `golang.org/x/sys/windows`; ~25 lines.
      Names come from `ProcessEntry32.ExeFile` (`[MAX_PATH]uint16`, trimmed
      at the first NUL via a new `exeFileToString` helper). No `OpenProcess`
      call anywhere. Snapshot handle closed via `defer windows.CloseHandle`.
      A failed `CreateToolhelp32Snapshot`/`Process32First` logs at
      `slog.Debug` and returns an empty map, matching the prior "never a
      failure" contract.
- [x] `github.com/shirou/gopsutil/v4` removed from `go.mod`; `go mod tidy`
      also dropped its now-unused transitive deps (`ebitengine/purego`,
      `go-ole/go-ole`, `lufia/plan9stats`, `power-devops/perfstat`,
      `tklauser/go-sysconf`, `tklauser/numcpus`, `yusufpapurcu/wmi`).
      `grep -rn gopsutil` across the repo now returns nothing.
- [x] `go run ./cmd/probe --count 1` compared before/after: same header,
      same `[windows] N visible` / `pid=... proc=... title=...` formatting
      and ordering, same `[microphone]`/`[webcam]` sections. Every PID
      present before the change resolved to the same process name after;
      the only diffs (a couple of extra window rows, an unread-counter
      tick) were real desktop activity between the two runs, not a
      regression.
- [x] **Test:** `TestProcessNamesIncludesSelf` (new,
      `platform/windows/process_test.go`) — the current process's own PID
      resolves to a non-empty name.

**Done when:** `go build/vet/test ./...` clean and probe output matches.
**Done** — clean on both counts. `-race` could not be run in this sandbox
(no C compiler); needs confirming on Windows CI.

### [x] 5.3 Bound every publish

**Touches:** `internal/mqtt/client.go`.

- [x] `Client.publish` now derives its ctx via `boundedPublishContext`,
      wrapping the caller's ctx with `context.WithTimeout(ctx,
      publishTimeout)` (`publishTimeout = 5 * time.Second`, a documented
      package constant). Factored into its own function purely so the
      bound is unit-testable without a live broker connection.
      `context.WithTimeout` always honours whichever deadline is sooner, so
      this only ever shortens an absent/looser caller deadline, never
      lengthens a tighter one. `New`'s connection-lifetime `ctx` handling
      (autopaho's own `NewConnection(ctx, ...)`) is untouched — this is
      purely the per-call publish path.
- [x] Added `PacketTimeout: publishTimeout` to `paho.ClientConfig`
      (confirmed the field exists and is what paho itself uses to bound a
      QoS 1/2 PUBACK wait, independent of the caller's ctx — defaults to
      10s if unset, which is looser than the new 5s bound). Kept in step
      with `publishTimeout` rather than carrying two independent numbers
      that could drift; it's a second, protocol-level guard that still
      fires even if a future refactor ever bypassed `Client.publish`'s own
      wrapper, in the same belt-and-suspenders spirit as this package's
      other four independent offline-state guards.
- [x] **Tests** (new `internal/mqtt/client_test.go` — no existing seam for
      faking `autopaho.ConnectionManager`, and a hand-rolled fake MQTT
      broker was judged out of scope for one test):
      `TestPublishTimeoutConstant` pins the 5s value;
      `TestBoundedPublishContextBoundsAnUnboundedCtx` asserts an otherwise-
      unbounded `context.Background()` gets a deadline within
      `publishTimeout`; `TestBoundedPublishContextNeverLoosensACallersDeadline`
      confirms a tighter caller deadline (50ms) still fires on schedule.

**Done when:** `go test ./internal/mqtt/...` passes. **Done** — all tests
pass, `go vet` clean.

### [x] 5.4 Log file growth

**Touches:** `cmd/callmqtt/main.go`.

- [x] `newLogger` calls `rotateLogIfLarge` before opening the log file: if
      the existing file is ≥ `maxLogSize` (5 MB), it is renamed to
      `<file>.1` (`os.Rename`, which overwrites any previous generation on
      both Windows and POSIX) before a fresh file is opened. One
      generation of history is kept; a missing file is not an error.
      Documented in `example.yaml` next to `logging.file`.
- [x] Combined with 3.1's rate-limited retry log, the broker-down case no
      longer writes one line per 2s, so this mainly guards long uptimes
      and `debug`-level logging.
- [x] **Repro:** `TestRotateLogIfLarge` in `cmd/callmqtt/main_test.go` —
      missing file is a no-op, a small file is left alone, an oversized
      file is rotated and overwrites a stale `.1`, with byte-for-byte
      content preserved in the rotated file.

**Done when:** `go test ./cmd/callmqtt/...` passes. **Done.**

---

## Phase 6 — The network gate is weaker than it claims

### [x] 6.1 Reject rules that can never match

**Touches:** `internal/config/config.go`, `internal/network/local.go`.

- [x] Expose from `network` what the current checker can populate
      (`LocalChecker{}.Capabilities()` → `{CIDR: true, SSID: false, …}`).
      Landed as `network.Capabilities{CIDR, SSID, BSSID, Gateway bool}` plus
      a `Capabilities() Capabilities` method added directly to the
      `network.Checker` interface (not a narrower optional-interface type
      assertion — only three implementers existed, `LocalChecker` and two
      test fakes, so widening the interface was the smaller, more honest
      change: every `Checker` now has to say what it can actually see, not
      just the one in production). `LocalChecker.Capabilities()` returns
      `{CIDR: true}` — SSID, BSSID and Gateway are equally unsupported
      today, not just SSID as the bullet above originally called out (see
      `internal/network/local.go`'s `Current`/`infosForInterfaces`, which
      never sets those three fields).
- [x] Put the check in `engine.New`, not `config.Validate`, as the bullet
      above suggested as the preferred seam: `New` already compiles the
      matcher and holds `opts.Checker`, so it's the one place that has both
      the rules and *this run's* checker without new plumbing. The actual
      rule-vs-capability logic is the exported, unit-testable
      `network.CheckCapabilities(rules []config.NetworkRule, caps
      Capabilities) error` in the new `internal/network/capabilities.go` —
      `config.Validate` doesn't know which checker will run, so it can't
      make this judgment itself. `CheckCapabilities` reports every
      offending rule at once (`errors.Join`, matching `Validate`'s own
      convention), and the message matches the bullet's own example
      verbatim for the ssids-only case: `rule "Home" matches on ssids only;
      SSID detection is not implemented on this platform, add a cidrs
      entry`. A rule mixing a supported and an unsupported matcher (e.g.
      `ssids` + `cidrs`) passes, because the supported field alone is
      enough for the rule to ever fire.
- [x] Updated both existing `Checker` fakes
      (`internal/engine/engine_test.go`, `internal/supervisor/supervisor_test.go`)
      to implement `Capabilities() network.Capabilities { return
      network.Capabilities{CIDR: true} }`, matching `LocalChecker`'s actual
      behaviour rather than claiming more than the real checker can do.
- [x] **Repro:** `internal/network/capabilities_test.go`
      (`TestCheckCapabilities`) — table-driven: ssids-only, bssids-only, and
      gateways-only rules are each rejected with a message naming the rule
      and the missing capability; two unsupported matchers together
      (`ssids`+`bssids`, no cidrs) are still rejected and both are named;
      `ssids`+`cidrs` passes; `cidrs`-only passes; `gateways`-only passes
      once the capability says `Gateway: true` (so the check is genuinely
      keyed off the checker's capabilities, not hardcoded to "cidrs is the
      only valid field"); a rule with no matchers at all is correctly left
      to `config.Validate`, not this function. `internal/engine/engine_test.go`
      (`TestNewRejectsRuleTheCheckerCanNeverMatch`) covers the wiring:
      `engine.New` itself now rejects an ssids-only rule, accepts
      ssids+cidrs, and accepts cidrs-only, against `fakeChecker`'s
      `{CIDR: true}` capabilities.

**Done when:** `go build ./...`, `go vet ./...`, and `go test ./...` are
clean, with and without `-tags tray`. **Done** — all four confirmed clean.
`-race` could not be run in this sandbox (no C compiler, `CGO_ENABLED=0`),
same limitation already noted for 3.2/3.3/5.2; still needs confirming on
Windows CI, which has the toolchain.

### [x] 6.2 Stop treating virtual adapters as evidence

**Touches:** `internal/network/local.go`, `internal/network/local_test.go`.

- [x] Skip interfaces whose name matches the known virtual set on
      Windows (`vEthernet`, `VirtualBox Host-Only`, `VMware`, `Hyper-V`,
      `WSL`, `Loopback`, `Bluetooth`) **and** interfaces without
      `FlagRunning`. Keep it a denylist with a comment; it's heuristics,
      say so. Landed as a package-level `virtualAdapterNames` slice plus
      `isVirtualAdapterName` (case-insensitive substring match) in
      `internal/network/local.go`, with a comment written in the same style
      as `platform/windows/windows.go`'s `enumMu` comment: it says plainly
      that this is a name-based heuristic, not a real classification — a
      renamed adapter dodges it, and a real adapter that happens to contain
      one of these words would wrongly be skipped — and that the correct
      fix is `platform/windows`'s `GetAdaptersAddresses`/`IfType`/
      `OperStatus` follow-up (still open, see below), not this list.
      `infosForInterfaces` now also skips any interface without
      `net.FlagRunning` (administratively up but not actually carrying
      traffic — e.g. a Wi-Fi adapter with no AP joined), documented inline
      as distinct from the pre-existing `FlagUp`/`FlagLoopback` check.
- [x] **Repro:** `internal/network/local_test.go` — updated the existing
      `TestInfosForInterfaces` fixture's "connected" interfaces to also set
      `FlagRunning` (they previously only set `FlagUp`, which would have
      made the new `FlagRunning` check silently break that test rather than
      exercise it) and added two new tests:
      `TestInfosForInterfacesSkipsVirtualAndNonRunningAdapters` (one case
      per denylist entry — `vEthernet (WSL)`, `VirtualBox Host-Only
      Network`, `VMware Network Adapter VMnet8`, `Hyper-V Virtual Ethernet
      Adapter`, a WSL-named adapter, `Bluetooth Network Connection`, a
      lowercase `vmware` match to prove case-insensitivity, plus an
      up-but-not-running Wi-Fi adapter) all report zero `Info`s despite
      carrying a plausible private address; and
      `TestInfosForInterfacesKeepsRealRunningAdapter`, asserting a genuine
      up-and-running, non-denylisted adapter is unaffected by either new
      check.
- [ ] Better: Windows `GetAdaptersAddresses` gives `IfType` and
      `OperStatus`; filter to `IF_TYPE_ETHERNET_CSMACD` /
      `IF_TYPE_IEEE80211` with `IfOperStatusUp`. That's a real
      `platform/windows/network.go` and the `win-platform` agent's job.
      Do the denylist now, file the proper version as a follow-up. **Not
      implemented here, by design** — this bullet is explicitly scoped to
      `platform/windows` and the `win-platform` agent; it stays open and
      unchecked as the tracked follow-up.

**Done when:** `internal/network/local_test.go` covers both the denylist
and the `FlagRunning` check via the existing `infosForInterfaces` seam
(synthetic interfaces/flags, no real OS calls). **Done** — `go test
./internal/network/...` is clean and includes both new tests; the
`GetAdaptersAddresses`-based real classification remains a deliberate,
tracked follow-up for `platform/windows`, not something silently dropped.

### [x] 6.3 Document the honest threat model

**Touches:** `README.md`, `SECURITY.md` (via the `release-ci` agent).

- [x] State plainly: matching is by local subnet only; `192.168.1.0/24`
      is not a unique identity; recommend gateway MAC/BSSID once
      implemented and, until then, a non-default home subnet.
- [x] Remove or mark "planned" every mention of SSID/BSSID matching in
      the design doc that reads as shipped.

**Done when:** a reader of the README or SECURITY.md comes away knowing
`cidrs` is the only matcher that works today and why a shared subnet is a
weak identity claim. **Done** — README.md's Privacy section gained a
paragraph stating CIDR-only matching, the shared-subnet risk, and a
recommendation to move off the router's default LAN range; the
`allowed_networks` row in the Configuration table now says `ssids`/`bssids`/
`gateways` are accepted but not implemented and that an SSID/BSSID/gateway-only
rule fails validation (matching 6.1's new behaviour, landing separately).
SECURITY.md got a new "Threat model & known limitations" section spelling out
the same gap plus the fail-closed/fail-open framing (off an allowed subnet:
publishes nothing; on a subnet sharing a home network's default range: treated
as home). `docs/In a call notification.md` — a 1472-line early design scrape,
not touched line-by-line — got one prominent status callout at the top
pointing to README.md/SECURITY.md as authoritative, plus fixes to its two most
concretely misleading claims: the "MVP v0.1" `✅ Allowed SSID` checklist (now
flagged not-shipped, CIDR-only) and the `callmqtt diagnose` sample output
(now marked illustrative/never-shipped, since no `diagnose` command or macOS
build exists). Validator run: `go run
./.claude/skills/readme-standards/scripts/validate_readme.go` → PASS (all
required and recommended sections present).

---

## Phase 7 — Smaller smells (batch into one "cleanup" PR)

- [x] **7.1** `onConnectionUp` comment says it republishes state; make it
      true — add a `SetStateSource(func() (Payload, bool))` the engine
      wires, and publish it after `online`. Then `retain: false` becomes
      a viable setting.
  - `internal/mqtt/client.go`: added `Client.SetStateSource(f func()
    (Payload, bool))` (stored on an `atomic.Pointer`, since it's read from
    autopaho's own goroutine on every reconnect). Extracted `onConnectionUp`'s
    detached body into `republish(ctx, cm connectionPublisher)` — a new
    unexported `connectionPublisher` interface (satisfied structurally by
    `*autopaho.ConnectionManager`) that makes the whole reconnect sequence
    unit-testable with a fake, no live broker needed. `republish` now
    publishes retained `online`, then discovery, then — if a state source is
    wired and reports `ok`, via a `payloadFromStatus`-shared marshal path —
    the current state, with each step logged independently so one failure
    doesn't skip the next.
  - `internal/engine/engine.go`: added `Status.EvaluatedAt`, `Engine.evaluated
    bool` (guarded by the existing `statusMu`), and `CurrentPayload() (mqtt.
    Payload, bool)` (`ok=false` until the first `evaluate()`). Factored
    `payloadFromStatus` out of `evaluate()` so the heartbeat/change publish
    path and `CurrentPayload` can't drift apart.
  - `internal/supervisor/supervisor.go`: `Publisher` interface gained
    `SetStateSource`; `build()` wires `pub.SetStateSource(eng.CurrentPayload)`
    right after constructing the engine.
  - Timestamp on a republish is the time the state was last *evaluated*
    (`Status.EvaluatedAt`), not the reconnect moment — an honest replay of
    stale-but-true information, documented on `payloadFromStatus`.
  - **Repro:** `internal/mqtt/client_test.go` —
    `TestRepublishPublishesOnlineDiscoveryThenState`,
    `TestRepublishSkipsStateWhenSourceReportsNotOK`,
    `TestRepublishSkipsStateWhenNoSourceIsWired`,
    `TestRepublishContinuesAfterAvailabilityFailure`, all exercising
    `republish` directly via a `fakeConnectionPublisher`.
    `internal/engine/engine_test.go`'s `TestCurrentPayloadReflectsLatestEvaluation`
    covers the `ok=false`-before-first-evaluate case.
    `internal/supervisor/supervisor_test.go`'s `TestBuildWiresEngineStateSource`
    confirms `build()` actually wires the closure end-to-end.

**Done when:** `go test ./internal/mqtt/... ./internal/engine/...
./internal/supervisor/...` passes, and reconnecting mid-call republishes the
current state alongside `online`. **Done** — `go build`, `go vet`, and
`go test` (with and without `-tags tray`) all clean across the whole repo.

- [x] **7.2** `platform/windows/dialog.go:182` — hoist
      `syscall.NewCallback(dialogWndProc)` to a package `var` like
      `windows.go` does. Use the `err` return of `.Call` instead of a
      separate `windows.GetLastError()` at :189 and :211.
  - Added `var dialogWndProcCallback = syscall.NewCallback(dialogWndProc)` at
    package scope, comment mirroring `windows.go`'s `enumCallback` reasoning
    (a callback slot is never released; allocating one per `show()` call would
    leak one every time a dialog is shown). `show()` now uses the package var.
  - Both `.Call()` sites (`procRegisterClassExW`, `procCreateWindowExW`) now
    capture their own `err` return instead of discarding it; the two
    `windows.GetLastError()` round-trips are gone. `LazyProc.Call`'s err is
    documented as always non-nil and always exactly `windows.Errno`, so the
    existing `ERROR_CLASS_ALREADY_EXISTS` comparison needed no type change —
    just needed the real return instead of a second syscall.
  - No new automated test: this package has no seam for real window creation
    (no `dialog_test.go`), and a GUI message-loop harness wasn't built for a
    callback-plumbing change. Verified via `go build -tags windows ./...` and
    `go vet -tags windows ./...`, both clean — same "manual/build-clean"
    allowance already used elsewhere in this file's own TODO history.

- [x] **7.3** `tray.openFile` — `windows.ShellExecute(0, "open", path,
      "", "", SW_SHOWNORMAL)` on Windows instead of `cmd /c start`.
  - `internal/tray/tray.go`'s `openFile` now delegates the Windows case to a
    new `openFileOS(path)` seam; darwin/other branches unchanged.
  - New `internal/tray/openfile_windows.go` (`//go:build windows && tray`):
    calls `windows.ShellExecute(0, verbPtr, pathPtr, nil, nil,
    windows.SW_SHOWNORMAL)` via `windows.UTF16PtrFromString`, matching
    `platform/windows`'s existing Win32-call convention. Errors logged at
    `slog.Debug` only, matching the original code's best-effort severity (it
    used to discard `cmd.Start()`'s error outright).
  - New `internal/tray/openfile_other.go` (`//go:build !windows && tray`): a
    no-op `openFileOS` so the package still compiles on darwin/linux under
    `-tags tray`; unreachable at runtime there.
  - Chose build-tag-file split over an in-file runtime branch, matching how
    `cmd/callmqtt`'s `dialog_windows.go`/`dialog_stub.go` already split
    OS-specific behaviour in this repo.
  - No test added for `openFileOS` itself (no existing seam, and invoking
    `ShellExecute`/`exec.Command` isn't worth a harness for a two-line OS
    call) — verified via `go build`/`go test -tags "windows tray"` plus a
    cross-compiled `GOOS=darwin`/`GOOS=linux` build of the affected packages.

- [x] **7.4** ConsentStore liveness: in `devicesInUse`, drop entries whose
      owning process (packaged → package family; NonPackaged → exe path)
      is not in the current process list. Requires 5.1's shared snapshot
      so it's free. Add a fixture: Teams mic entry with `Stop=0` but no
      `ms-teams.exe` running → not in use.
  - `platform/windows/consent_pure.go`: added `consentEntry`,
    `filterLiveConsentEntries(entries, runningExeNames)`,
    `runningExeNameSet(procNames map[uint32]string)`, and
    `baseExeNameLower(path)` — all pure, no registry/syscall dependency
    (build and test on every `GOOS`, matching the file's existing
    `isLive`/`unmangleNonPackagedKey` split).
  - `platform/windows/consent.go`: `devicesInUse` now takes `procNames
    map[uint32]string`, collects `consentEntry` values while walking the
    registry, and filters through `filterLiveConsentEntries` before sorting.
    `AppsUsingMicrophone`/`AppsUsingWebcam` now take and forward `procNames`.
  - `internal/detectors/detectors.go`: `Snapshot`'s two fields changed to
    `func(procNames map[uint32]string) []string`; `observe()` passes the
    `names` map it already computed via `s.ProcessNames()` — free, per the
    TODO's own framing, since `observe()` already had that map in hand before
    calling either function.
  - **Packaged gap closed (issue #8).** Originally only `NonPackaged`
    entries were matched against the running-process set, because packaged
    (MSIX) entries are keyed by package family name and resolving that
    needs `GetPackageFamilyName` on an `OpenProcess` handle per PID. That
    gap left a stale `MSTeams_*` mic entry (Teams crashed mid-call, Windows
    never wrote a Stop time) trusted for hours or days. Now
    `runningProcesses` (consent.go) resolves exe name, package family name
    and creation time per PID — hand-rolled `kernel32!GetPackageFamilyName`
    plus `GetProcessTimes` — and `filterLiveConsentEntries` takes it as a
    lazy resolver, invoked at most once per poll and only when some entry is
    live, so idle polls pay nothing. A live entry is kept only if a running
    owner (same exe / same family) was created no later than the entry's
    `LastUsedTimeStart` (+10 s `consentStartSlack`): a relaunched client is
    younger than the capture it would otherwise be credited with, which is
    the one shape the process-name check alone could never catch, and it
    now applies to NonPackaged apps (Zoom relaunched after a crash) too.
  - Downstream call-site fixes for the new signature: `platform/windows/
    stub_other.go`'s non-Windows stubs, and `cmd/probe/main.go` (already
    computed `names := platformwindows.ProcessNames()` a few lines above its
    call, so threading it through was likewise free).
  - **Repro:** `platform/windows/consent_pure_test.go` —
    `TestFilterLiveConsentEntries_NonPackagedDroppedWhenProcessNotRunning`
    (the TODO's exact fixture: a Teams mic entry with `Stop=0` dropped when
    `ms-teams.exe` isn't running),
    `TestFilterLiveConsentEntries_NonPackagedKeptWhenProcessRunning`,
    `TestFilterLiveConsentEntries_PackagedStaleAfterRelaunch` (issue #8: the
    relaunched-after-crash shape, the genuine-call shape, family mismatch,
    the slack boundary, unknown times), its NonPackaged twin, the laziness
    contract, a table-driven `TestFilterLiveConsentEntries` (not-live,
    case-insensitivity, packaged kept/dropped by family, mixed cases), and
    `TestBaseExeNameLower` — all pure, no registry access.

**Done when:** `go build -tags windows ./... && go vet -tags windows ./...`
and `go test -tags windows ./platform/windows/... ./internal/tray/...
./internal/detectors/...` (with and without `-tags tray`) pass. **Done** for
7.2–7.4 — all clean; the packaged-app gap 7.4 originally left open was
closed for issue #8 (see above).

- [x] **7.5** TLS: add `mqtt.tls.ca_file` (PEM) so self-signed brokers
      don't need `insecure_skip_verify`. Optional `cert_file`/`key_file`.
  - `internal/config/config.go`: `TLS` struct gained `CAFile`, `CertFile`,
    `KeyFile`, each documented. `Validate()` rejects `cert_file` set without
    `key_file` or vice versa (both-or-neither). All three fields go through
    the same `${VAR}` expansion as other secret/path fields, and `Load()`
    resolves them relative to the config file's directory (mirroring
    `rules_file`'s resolution) via a new `resolveRelative(baseDir, path)`
    helper; `Parse()` alone (no file path available) leaves them unresolved,
    documented on `Load`.
  - `internal/mqtt/client.go`: extracted `buildTLSConfig(t config.TLS)
    (*tls.Config, error)` from `New`. `CAFile` set: reads the PEM, builds an
    `x509.CertPool` via `AppendCertsFromPEM`, sets `RootCAs` — a read/parse
    failure fails `New` fast (wrapped error), matching this function's
    existing config-problem-is-fatal pattern. `CertFile`+`KeyFile` both set:
    `tls.LoadX509KeyPair`, added to `Certificates`, same fail-fast treatment.
  - `internal/config/example.yaml`: added a commented `tls:` block under
    `mqtt:` documenting `ca_file`, `cert_file`/`key_file`, and
    `insecure_skip_verify`.
  - **Repro:** `internal/config/config_test.go` — `TestValidationRejects`
    gained the cert/key-without-its-pair cases; added
    `TestTLSCertAndKeyBothSetIsAccepted`, `TestTLSFieldsParsed`,
    `TestLoadResolvesTLSPathsRelativeToConfigDir`. `internal/mqtt/
    client_test.go` — a `generateSelfSignedCert` helper (ECDSA P256,
    generated fresh per test, no checked-in fixture PEMs) plus
    `TestBuildTLSConfigDisabledReturnsNil`, `TestBuildTLSConfigLoadsCAFile`,
    `TestBuildTLSConfigRejectsMissingCAFile`,
    `TestBuildTLSConfigRejectsGarbageCAFile`,
    `TestBuildTLSConfigLoadsClientKeypair`,
    `TestBuildTLSConfigRejectsBadClientKeypair`,
    `TestBuildTLSConfigOnlyCertFileSetIsIgnored` (documents that
    `buildTLSConfig` itself stays permissive about a lone cert/key —
    `config.Validate` is what actually rejects that),
    `TestBuildTLSConfigInsecureSkipVerifyPassthrough`.

**Done when:** `go test ./internal/config/... ./internal/mqtt/...` passes,
with and without `-tags tray`. **Done** — all clean.

- [x] **7.6** `expire_after` guard: `Validate` should fail if
      `heartbeat_seconds * 1.5 < detect_seconds * 2` — not possible with
      current minimums, but the invariant lives in two packages and
      nothing ties them.
  - `internal/config/config.go`: added `HeartbeatExpireSafetyFactor = 1.5`
    (exported) and `minDetectCyclesBeforeExpire = 2` (unexported), and a new,
    independent check in `Validate()` (added after, not replacing, the
    existing per-field `must be at least 1` checks):
    `float64(c.Poll.HeartbeatSeconds)*HeartbeatExpireSafetyFactor <
    float64(c.Poll.DetectSeconds)*minDetectCyclesBeforeExpire` fails with a
    message naming both fields and explaining the risk (Home Assistant's
    `expire_after` window lapsing between real detect cycles).
  - `internal/mqtt/discovery.go`: its local `heartbeatSafetyFactor` is now
    `const heartbeatSafetyFactor = config.HeartbeatExpireSafetyFactor` instead
    of a parallel magic number — since `internal/mqtt` already imports
    `internal/config` (never the reverse), the constant lives in `config` and
    `discovery.go` references it directly, so the two can no longer drift
    silently. Comments in both files cross-reference each other by name.
  - **Repro:** `internal/config/config_test.go` — a `TestValidationRejects`
    case (`detect_seconds: 5, heartbeat_seconds: 2`, each individually ≥ its
    own minimum) asserting the combined error names both fields; and
    `TestHeartbeatDetectBoundaryAccepted`, pinning the exact boundary
    (`heartbeat=4, detect=3` → `4*1.5 == 3*2 == 6`, must still pass) and
    confirming the shipped defaults don't trip the new check.

- [x] **7.7** `slugify`: trailing-hyphen handling walks `b.String()` on
      every non-alnum rune (quadratic on pathological input). Track
      `lastWasSep bool` instead.
  - `internal/config/config.go`: replaced the per-rune `b.String()` +
    `strings.HasSuffix` check with a `lastWasSep bool` local, initialized
    `true` (covers both "nothing written yet" and "last char was a
    separator" — verified against existing test cases, e.g. `""` → `""` and
    `"--weird--"` → `"weird"`). Pure refactor, no semantic change; the final
    `strings.Trim(...)` (linear, one-time) is untouched, matching the TODO's
    own scoping to the per-rune call.
  - **Repro:** `TestSlugify` gained a 10,000-char all-`!` input (→ `""`) and a
    mid-string separator-run case (`"a" + 500×"!" + "b"` → `"a-b"`). Added
    `BenchmarkSlugify` (10,000-char separator-only input): `go test
    ./internal/config/... -bench=Slugify -benchtime=1x` → `24300 ns/op`,
    confirming linear-time behaviour on the pathological case.

**Done when:** `go test ./internal/config/... ./internal/mqtt/...` passes.
**Done** — all clean, including the new benchmark.

- [x] **7.8** `supervisor.stop` 5s wait + `Close` 5s shutdown ctx +
      publish → the worst-case quit is ~15s of "why is it still in the
      tray". Cap total shutdown at 5s by sharing one deadline.
  - `internal/supervisor/supervisor.go`: `stop(ctx, gen)` now derives one
    `stopCtx, cancel := context.WithTimeout(ctx, stopTimeout)` at the top,
    uses `stopCtx.Done()` in place of the old bare `time.After(5*time.
    Second)` for the `gen.done` wait, and passes that *same* `stopCtx` to
    `gen.pub.Close(...)` — so whatever budget the `gen.done` wait already
    consumed, `pub.Close` only gets what's left, not a fresh 5s on top.
    `stopTimeout` is a package `var` (5s default) rather than a `const`,
    specifically so tests can shrink it without a real multi-second sleep.
  - Traced the worst case with the change applied: `gen.done` never fires
    (stuck engine, an independent pre-existing bug) → `stopCtx` expires at
    `stopTimeout` (further bounded if the caller's own `ctx` is tighter, same
    "sooner of the two" idiom as `mqtt.boundedPublishContext`) →
    `gen.pub.Close(stopCtx)` is called with an already-expired ctx;
    `mqtt.Client.Close`'s own ctx-aware publish/disconnect calls are expected
    to fail fast on it rather than block, falling back to the Will as the
    documented guard for exactly this "no time left for a clean offline
    publish" case. Total worst case: bounded to ~`stopTimeout` (5s), not
    ~15s. Concluded `internal/mqtt/client.go` needs no change — its own
    internal bound just becomes redundant-but-harmless once fed an
    already-expired ctx.
  - `stop`'s doc comment now states the ~5s total worst-case bound explicitly
    and the fail-fast-over-a-stuck-engine reasoning.
  - **Repro:** `internal/supervisor/supervisor_test.go`'s
    `TestStopBoundsTotalShutdownTime` — a `*generation` built directly with a
    `done` channel that's never closed (stuck-engine stand-in) and a
    `fakePublisher` extended with a `closeDelay` field that blocks `Close`
    unless its ctx is cancelled first (mirroring `mqtt.Client.Close`'s real
    behaviour). The test temporarily shrinks the package `stopTimeout` var to
    100ms (restored via `t.Cleanup`) so the assertion is deterministic and
    fast (~0.14s) instead of sleeping for a real 5s+, with a 2s hard
    `time.After` guard so a genuine regression fails fast instead of hanging
    the suite. Asserts elapsed time stays near the shrunk `stopTimeout`
    rather than `stopTimeout + closeDelay`.

**Done when:** `go test ./internal/supervisor/... ./internal/mqtt/...`
passes and a stuck engine no longer stretches shutdown past ~5s. **Done** —
all clean.

**Phase 7 overall:** `go build ./...`, `go vet ./...`, and `go test ./...`
are clean, with and without `-tags tray`, across the whole repo (all 7.1–7.8
changes landed together and were verified together, not just individually).

---

## Phase 8 — macOS support

### Why it doesn't work today

Every real signal lives in `platform/windows/`. The non-Windows half of
that package (`stub_other.go`) is a no-op stub that returns "no evidence"
for all four signals — it exists only so `cmd/probe` cross-compiles. A
`GOOS=darwin` build therefore launches, shows a menu-bar icon, connects to
the broker, and then publishes `inactive` **forever, with full
confidence** — a false negative Home Assistant would trust. On top of
that, `.goreleaser.yaml` builds `goos: [windows]` only, so no darwin
asset exists. This is not a missing-docs problem; the darwin adapter has
to be written. The original design doc (`docs/In a call notification.md`,
"platform/darwin/") always intended it.

### Requirements for the target machine

The target is a **managed work Mac**, which fixes these requirements:

- **Install = copy one file.** A single Mach-O binary, no installer, no
  `.pkg`, no `sudo`. It lives in the user's home folder at a stable path
  and is launched at login via a user LaunchAgent
  (`~/Library/LaunchAgents/`). Nothing in install, autostart, permission
  grant, or uninstall may require admin rights.
- **The permission ask is exactly one:** Screen Recording, for window
  titles. **Accessibility is not required** and the design below must
  not drift into needing it (`CGWindowListCopyWindowInfo` is
  Screen-Recording-gated; the AXUIElement route the old design doc
  sketched is what would need Accessibility, and it is out of scope).
- **Degrade, don't fail.** Without Screen Recording the app must still
  run on process evidence alone and say so in the tray, never crash or
  log-spam.

### The four obstacles, and why they gate the whole phase

None of these is a Go problem. All four are properties of the target
machine's management policy, and every one can be tested **before any
darwin code exists** (8.0):

1. **Gatekeeper.** An unsigned binary downloaded by a browser is
   quarantined. On macOS 15+ the right-click → Open bypass no longer
   exists; the only path is System Settings → Privacy & Security →
   "Open Anyway", and MDM can disable that override outright. On a
   managed machine this may be a hard wall without IT involvement.
2. **Privacy permissions (TCC).** Window titles need Screen Recording.
   MDM **cannot** silently grant it — it can only *deny* it for
   everyone, or leave it for the standard user to grant. If the org's
   PPPC profile denies it, titles are unobtainable and only
   process-presence evidence remains (which the rules deliberately cap
   below the `active` threshold — see Phase 6). TCC grants for an
   *unsigned* binary are keyed to its path and code hash, so moving or
   rebuilding the binary silently revokes the grant.
3. **EDR.** An unsigned binary that enumerates processes and windows,
   persists via LaunchAgent, and opens an outbound connection on 1883 to
   a non-corporate host is the textbook "persistence + beaconing"
   pattern EDR products flag. Being open source helps only if someone
   at IT reads it.
4. **Policy.** Independently of whether it *can* run, running
   self-built monitoring software on a managed machine may violate an
   acceptable-use policy. That is a conversation, not a config option.

Hard constraints carried over from the Windows build, none negotiable
without a separate decision:

- **No cgo.** Releases build with `CGO_ENABLED=0`. The vendored
  `third_party/systray` already talks to AppKit through
  `github.com/go-webgpu/goffi` (dlopen + ffi, no cgo); every darwin
  syscall below must use the same mechanism or `golang.org/x/sys/unix`.
- **Titles never logged above Debug.** Same privacy contract as
  `platform/windows.WindowInfo`.
- **Absent signal reports "no evidence", never an error.** Same contract
  as `stub_other.go`.

Ordered so that 8.0 costs nothing and can kill the phase; 8.2–8.3 then
produce something a Mac user can run (`cmd/probe`) before any rule,
packaging, or docs work starts — darwin rules cannot be authored without
real captures, exactly as Phase 6's Windows rules were.

### [ ] 8.0 Go / no-go on the target machine — no code, ~30 minutes

Do this on the **actual work Mac**, not a personal one. Every check
below uses tools that already exist. If any check is a hard "no", stop
and either get IT involved or shelve the phase; do not start 8.1.

- [ ] **Policy first.** Ask IT (or read the acceptable-use policy)
      whether running a self-built, open-source, unsigned utility that
      reads window titles and publishes a boolean to a home MQTT broker
      is acceptable. Offer them the repo link and the SECURITY.md threat
      model. Record the answer here.
- [ ] **Is the machine MDM-managed, and what does the profile say?**
      `system_profiler SPConfigurationProfileDataType` (no admin needed)
      lists installed profiles. Look for a `com.apple.systempolicy.control`
      payload (Gatekeeper: `DisableOverride`, `AllowIdentifiedDevelopers`)
      and a `com.apple.TCC.configuration-profile-policy` payload with a
      `ScreenCapture` entry (`Allowed: false` = hard no for titles).
- [ ] **Gatekeeper canary.** Cross-compile today's probe from Windows —
      `GOOS=darwin GOARCH=arm64 go build -o probe-darwin ./cmd/probe`
      (use `amd64` for an Intel Mac) — transfer it via a browser download
      (so it gets the quarantine attribute; `curl`/AirDrop/`scp` don't,
      which would make the test meaningless), `chmod +x`, and run it. It
      will print empty results (that's the stub) but it exercises
      Gatekeeper exactly as the real binary would. Note which of these
      worked: ran outright / "Open Anyway" in Settings / `xattr -d
      com.apple.quarantine` / nothing.
- [ ] **Screen Recording canary.** System Settings → Privacy & Security →
      Screen Recording. If the list is editable and the `+` button is
      enabled, a standard user can grant it. If it's greyed out or shows
      "managed by your organisation", titles are off the table and the
      phase is worth doing only if process-only evidence is acceptable
      (it isn't, per the Phase 6 thresholds — so that's a no-go).
- [ ] **EDR presence.** `ls /Library/SystemExtensions /Applications |
      grep -iE 'crowdstrike|falcon|sentinel|carbon|defender|jamf'` and
      `systemextensionsctl list`. If an EDR is present, expect the probe
      canary above to be flagged or quietly killed; check whether it was.
      An EDR that tolerates the canary is a good sign, not a guarantee.
- [ ] **Network path.** The whole point is publishing from the *home*
      network. Confirm the work Mac actually joins the home Wi-Fi (not
      always-on VPN with no split tunnelling) and can reach the broker
      on 1883. `allowed_networks` will keep it silent on corporate
      networks; VPN `utun*` interfaces are excluded in 8.5.

**Done when:** each bullet has a recorded result pasted under it, and the
signing decision in 8.1 has been made *from* those results rather than
from preference.

### [ ] 8.1 Decide the three things only the maintainer can decide

Decide these on the issue before writing code; each one changes the
shape of items below.

- [ ] **Code signing — decided by the 8.0 Gatekeeper canary.**
      Notarization requires an Apple Developer ID (paid, yearly) and a
      signing step in CI. Two outcomes:
      - *Canary ran (outright, via "Open Anyway", or via `xattr`) and IT
        is fine with that path:* ship unsigned in this phase, document
        the exact path that worked, and keep signing as 8.11.
      - *Canary blocked and override disabled by MDM:* notarization moves
        into this phase as a prerequisite of 8.9, and even then IT may
        need to allow-list the Team ID. Budget for both the account and
        the IT ticket.
      Either way, **never ad-hoc sign in CI**: an ad-hoc signature
      changes every build, and TCC keys the Screen Recording grant to the
      code signature, so the user would be re-prompted on every upgrade.
- [ ] **Mic/camera attribution.** On Windows the ConsentStore names the
      *process* holding the mic, which is what `mic_process_regex` and
      `cam_process_regex` match. macOS exposes only a device-wide boolean
      from user space: CoreAudio `kAudioDevicePropertyDeviceIsRunningSomewhere`
      on the default input device, and CoreMediaIO
      `kCMIODevicePropertyDeviceIsRunningSomewhere` for cameras. Per-process
      attribution needs private APIs or parsing `log stream`, neither of
      which is acceptable. **Recommendation:** extend
      `rules.Observation` with `MicActive bool` / `CamActive bool`
      (unattributed), and let a rule's `mic_process_regex` match against
      the unattributed flag *only when the rule's `process_names` is
      also running* — i.e. "the mic is on and Teams is the only call app
      open" counts as Teams-mic. Two call apps open at once becomes
      ambiguous evidence and scores as neither, which is the same
      conservative bias the Windows rules already take (false `active`
      is worse than a missed call).
      The same decision covers the newer **audio-out** signal
      (`Snapshot.AppsRenderingAudio`, added for browser-based Meet to
      tell a joined call from its pre-join lobby). macOS can answer "is
      the default output device running" via the same CoreAudio property
      on the output device, again without naming the process. Since
      `AppsRenderingAudio` is already optional (`nil` = no source), the
      darwin adapter may leave it `nil` in the first release and accept
      that Meet-in-a-browser detection is weaker on macOS; say so in the
      FAQ (8.10).
- [ ] **Window-title permission.** `CGWindowListCopyWindowInfo` returns
      owner PID and owner name without any permission, but `kCGWindowName`
      (the title) is empty unless the app has **Screen Recording**. There
      is no narrower entitlement, and Accessibility does not substitute
      for it. **Recommendation:** accept it, prompt once via
      `CGRequestScreenCaptureAccess` on first tray launch, and document
      plainly in README's Privacy section that the permission is used only
      to read window *titles* (the app never captures pixels). Without the
      grant the darwin adapter still returns windows with empty titles, so
      process-only evidence keeps working.

**Done when:** all three are answered on the tracking issue and the
answers are pasted into this section.

### [ ] 8.2 Make the platform seam OS-neutral

`internal/detectors.Snapshot` already injects the four platform calls as
functions, so the detectors and engine don't change. What's wrong is the
naming: `platformwindows.WindowInfo` is the type every caller uses, and
`detectors.WindowsSnapshot()` / `cmd/probe` import `platform/windows`
unconditionally.

- [ ] New package `platform` (root, pure Go, no build tags) holding
      `type WindowInfo struct{ PID uint32; Title string }`. Keep the
      "never log above Debug" comment with it.
- [ ] `platform/windows.WindowInfo` becomes a type alias for
      `platform.WindowInfo` so nothing else has to change in that package.
- [ ] Rename `detectors.WindowsSnapshot()` → `detectors.PlatformSnapshot()`
      and split its body into `snapshot_windows.go`, `snapshot_darwin.go`
      (8.3) and `snapshot_other.go` (returns the current stub behaviour).
      Delete `platform/windows/stub_other.go`: once no non-Windows file
      imports `platform/windows`, the stub has no reason to exist.
- [ ] `cmd/probe` and `cmd/callmqtt` import only `detectors.PlatformSnapshot`,
      never a `platform/<os>` package directly.

**Repro:** `GOOS=darwin go build ./...` and `GOOS=linux go build ./...`
still succeed *and* `grep -rn 'platform/windows' --include=*.go cmd/
internal/` returns only files with a `//go:build windows` line.

**Done when:** the repro holds, `go test ./...` is unchanged on Windows,
and `detectors_test.go` compiles on every GOOS (it fakes `Snapshot`, so
it should already).

**Touches:** `platform/platform.go` (new), `platform/windows/windows.go`,
`platform/windows/stub_other.go` (deleted), `internal/detectors/`,
`cmd/probe/main.go`.

### [ ] 8.3 `platform/darwin`: process names and window list

The two signals that need no permission (process list) or exactly one
(titles). Same four-function contract as `platform/windows`, same "no
evidence, no error" behaviour.

- [ ] `process.go` — `ProcessNames() map[uint32]string` via
      `unix.SysctlKinfoProcSlice("kern.proc.all")` from `golang.org/x/sys/unix`
      (already a dependency). Report `kp_proc.p_comm`, which is the
      executable's basename truncated to 16 bytes — so `"Microsoft Teams"`
      arrives as `"Microsoft Teams"` but longer names are cut. Decide and
      document whether to use `proc_pidpath` (via ffi, full path) instead;
      the 5.2 comment on why one snapshot per tick matters applies here
      verbatim.
- [ ] `windows.go` — `VisibleWindows() []platform.WindowInfo` via
      `CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly |
      kCGWindowListExcludeDesktopElements, kCGNullWindowID)` through
      goffi, reading `kCGWindowOwnerPID`, `kCGWindowName` and
      `kCGWindowLayer` (keep layer 0 only: menu-bar extras, the Dock and
      Control Center overlays all live on other layers and would be
      noise). Release the CFArray. Serialise with a mutex for the same
      reason `platform/windows.enumMu` exists. **No AXUIElement calls in
      this package, ever** — see Requirements.
- [ ] `permissions.go` — `ScreenRecordingGranted() bool` via
      `CGPreflightScreenCaptureAccess`, and `RequestScreenRecording()` via
      `CGRequestScreenCaptureAccess`. Nothing in this package calls
      Request itself; the tray does (8.6).
- [ ] `consent.go` — `AppsUsingMicrophone` / `AppsUsingWebcam` per the
      8.1 decision. If the decision is "unattributed boolean", these two
      return `nil` and the booleans go through the new `Observation`
      fields instead; either way the file carries a comment explaining
      why macOS cannot name the process.
- [ ] `audio.go` — `AppsRenderingAudio` per the 8.1 decision: either
      `nil` (no source; Meet lobby vs call is then indistinguishable on
      macOS) or the unattributed CoreAudio output-device boolean routed
      the same way as mic/cam. The contract is now five functions, not
      four — keep `snapshot_darwin.go` in step with whatever
      `snapshot_windows.go` exposes.
- [ ] `snapshot_darwin.go` in `internal/detectors` wires it up.

**Repro:** on a real Mac, `go run ./cmd/probe` prints the same shape of
output as on Windows: a PID→name map, a window list with titles once
Screen Recording is granted (and with empty titles, not an error, before
it is), and the mic/cam section.

**Done when:** the repro holds; `go vet` is clean under `GOOS=darwin`;
the ffi call sites are covered by a `_test.go` that at least exercises
them once on `macos-latest` in CI (8.8) so a broken dlopen symbol name
fails CI rather than a user's first launch.

**Touches:** `platform/darwin/` (new), `internal/detectors/snapshot_darwin.go`
(new), `go.mod` (goffi moves from indirect to direct).

### [ ] 8.4 Capture real fixtures on a Mac and author the darwin rules

`internal/rules/rules.yaml` is written entirely against Windows process
names (`ms-teams.exe`, `Zoom.exe`, `Slack.exe`) and Windows title
formats. None of that can be assumed to hold on macOS: the Teams
executable is `MSTeams` inside `Microsoft Teams.app`, Zoom's is
`zoom.us`, Slack's is `Slack`, and the window title conventions have to
be observed, not guessed.

- [ ] Run `cmd/probe` on a Mac through the same scenario list Phase 6
      used (`testdata/probe/*.txt`): app open no call, in call, generic
      meeting title, huddle, call ended but window still open. Store
      them as `testdata/probe/darwin-*.txt`.
- [ ] Extend the rule schema so a rule can carry per-OS process names:
      either `process_names` gains a nested `darwin:`/`windows:` form, or
      the simpler option, both names in one list (a `.exe` name can never
      match on macOS and vice versa, so a combined list is harmless).
      **Recommendation:** the combined list — fewer schema changes, and
      the rules-file validator from 4.2 needs no new cases.
- [ ] Add darwin title regexes with the same captured-title comments the
      Windows rules have, and the same veto style for known idle windows.
- [ ] If a capture shows an app whose call state is *not* visible in any
      window title (Slack huddles are the likely case), record that as a
      known gap in README's FAQ rather than reaching for Accessibility.
- [ ] Fixture-driven regression tests in `rules_test.go` for each darwin
      capture, mirroring the existing `teams-idle-meet-window-only` test.
      Use the `detector-rules` subagent for this item — it owns
      `rules.yaml` and the fixture tests.

**Repro:** the new fixture tests fail against today's `rules.yaml`
(nothing matches a darwin process name) and pass after.

**Done when:** every darwin fixture has a test, every Windows fixture
still passes, and no darwin rule can reach the `active` threshold on
process presence alone (same 0.70 bar as Windows).

**Touches:** `internal/rules/rules.yaml`, `internal/rules/rules_test.go`,
`testdata/probe/darwin-*.txt`, possibly `internal/rules/rules.go` if the
schema changes.

### [ ] 8.5 Network gate: darwin interface names

`internal/network/local.go`'s `virtualAdapterNames` list is Windows
vocabulary (`vEthernet`, `Hyper-V`, `WSL`). On macOS the interfaces that
must never count as evidence are `lo0`, `utun*` (VPN / iCloud Private
Relay — and on a work Mac, the corporate VPN), `awdl*` and `llw*`
(AirDrop / Wi-Fi Aware), `bridge*`, `vmnet*`, `anpi*`, `ap1`, and
`gif*`/`stf*`. `en0` is usually Wi-Fi and `en1`+ can be Thunderbolt/USB
Ethernet, but that isn't guaranteed, so don't special-case `en*`.

- [ ] Add the darwin prefixes to the skip list, either by splitting the
      list per GOOS or by keeping one list (a name like `utun3` cannot
      collide with anything on Windows). Prefix match, not substring —
      `ap1` as a substring would hit real names.
- [ ] Re-read the 6.2 comment about "the proper fix is classifying by
      IfType" and note that on darwin the equivalent is
      `SCNetworkInterfaceGetInterfaceType` via SystemConfiguration; file
      it under the same follow-up, don't do it here.

**Repro:** table test in `local_test.go` with `utun3`, `awdl0`, `en0`,
`bridge100` — the first three skipped, `en0` kept.

**Done when:** the repro passes on every GOOS (the code is pure Go, so
run it on Windows too).

**Touches:** `internal/network/local.go`, `internal/network/local_test.go`.

### [ ] 8.6 Tray, startup, dialogs and file-open on darwin

Everything the tray needs that is currently a `!windows` stub. All of it
must work as a standard (non-admin) user — that is the Requirements
section, restated as an acceptance criterion.

- [ ] **Verify the vendored systray darwin backend actually works** with
      the menu this app builds — checkbox items, icon swaps on state
      change, the Quit item — before touching anything else. AppKit
      requires `systray.Run` on the main OS thread; confirm
      `tray.Run` is reached from `main` without an intervening goroutine
      on darwin, or add `runtime.LockOSThread` in an `init()` in
      `cmd/callmqtt` under `//go:build darwin`. If the backend is broken,
      fixing it lives in `third_party/systray/` and gets its own item.
- [ ] `cmd/callmqtt/startup_darwin.go` — `IsEnabled`/`Enable`/`Disable`
      by writing/removing
      `~/Library/LaunchAgents/com.crs2007.callmqtt.plist` (`RunAtLoad`,
      `ProgramArguments` = the absolute path of the running binary, as
      the Windows registry entry does). User domain only — never
      `/Library/LaunchAgents` or `LaunchDaemons`, which need admin. Don't
      call `launchctl load` on Enable — the plist is picked up at next
      login, which is what "start at login" means; do call `launchctl
      bootout gui/$UID/com.crs2007.callmqtt` on Disable so a stale job
      isn't left loaded. Narrow `startup_stub.go` to `!windows && !darwin`.
- [ ] `cmd/callmqtt/dialog_darwin.go` — `reportError` via `osascript -e
      'display alert ...'` (a subprocess is fine for a fatal-error path
      that runs once). `brokerDialog` stays the stub: the tray's
      nil-Dialog fallback already opens the config file, and building an
      NSAlert-with-text-fields through ffi is not worth it for a first
      release. Narrow `dialog_stub.go` to `!windows && !darwin`.
- [ ] `internal/tray/openfile_darwin.go` — `openFileOS` runs
      `open <path>`; and remove the `runtime.GOOS == "windows"` gate in
      `openFile` so darwin reaches it. Narrow `openfile_other.go` to
      `!windows && !darwin && tray`.
- [ ] On first tray launch on darwin, if `ScreenRecordingGranted()` is
      false, call `RequestScreenRecording()` once (it opens the system
      prompt) and add a persistent, disabled menu line
      "Window titles: permission needed" whose click opens the Privacy
      pane (`open x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture`).
      Never re-prompt on a timer. If the pane is MDM-locked the line
      still tells the user *why* detection is weak, which is the honest
      outcome.
- [ ] Log a one-line warning at startup if the binary's path differs
      from the one recorded in the LaunchAgent plist: the user moved it,
      and the TCC grant (keyed to path + hash for unsigned binaries) is
      now gone. Point at the FAQ entry from 8.10.

**Repro:** manual, on a Mac, as a standard user — toggle every checkbox,
Enable start-at-login and confirm the plist appears and survives a
logout/login, Quit publishes `offline`.

**Done when:** the manual repro passes and `go build -tags tray ./...`
under `GOOS=darwin` succeeds with no `!windows` stub compiled in for a
feature that now has a darwin file. Keep the `tray_test.go` fakes working
on all three GOOS values.

**Touches:** `cmd/callmqtt/{startup,dialog}_darwin.go` (new),
`cmd/callmqtt/{startup,dialog}_stub.go`, `internal/tray/openfile_darwin.go`
(new), `internal/tray/openfile_other.go`, `internal/tray/tray.go`,
possibly `third_party/systray/`.

### [ ] 8.7 Config, log and install paths on darwin

`os.UserConfigDir()` already resolves to
`~/Library/Application Support/callmqtt/` on darwin and `save.go`
already documents the 0600 mode, so the code needs little. What's
missing is the log location, the install location, and the docs.

- [ ] Pick **one documented install path** for the binary and use it
      everywhere (README, LaunchAgent example, FAQ). Recommendation:
      `~/Library/Application Support/callmqtt/callmqtt` — beside the
      config, user-writable, and stable, which matters because an
      unsigned binary's TCC grant dies when it moves. `~/Applications/`
      is the alternative if a Finder-visible location is preferred.
- [ ] Confirm the log path (`logPath` in `main.go`) lands somewhere
      sensible on darwin — `~/Library/Logs/callmqtt/callmqtt.log` is the
      platform convention; next to the config is acceptable. Pick one and
      make `--help` say so.
- [ ] `config init` must print the darwin path in its "edit this file"
      message, not `%APPDATA%`.

**Repro:** `go test ./cmd/callmqtt/... ./internal/config/...` with a
table case per GOOS for the path-printing helper.

**Done when:** running `callmqtt init` on a Mac prints a path the user
can copy into `open`.

**Touches:** `cmd/callmqtt/main.go`, `internal/config/config.go`.

### [ ] 8.8 CI: a real macOS job

- [ ] Add `test-macos` on `macos-latest` to `.github/workflows/ci.yml`:
      `go vet ./...`, `go build ./...`, `go build -tags tray ./...`,
      `go test -race ./...`. The hosted runner has no Teams and grants no
      TCC permissions, so every darwin unit test must pass with "no
      evidence" results — that is the contract 8.3 promises, and this job
      is what enforces it.
- [ ] Also cross-compile `darwin/amd64` (the runner is arm64) as a build
      check, mirroring the existing `windows/arm64` step.
- [ ] Delete the `cross-compile-darwin` ubuntu job; a real runner
      supersedes it. Update its comment's reasoning where it's referenced.
- [ ] Keep `windows-latest` as the required check; add `macos-latest`
      as required only after it has been green for a full release cycle.

**Done when:** CI is green on a PR that touches `platform/darwin/`, and
a deliberately misspelled ffi symbol name in a scratch branch makes the
macOS job red.

**Touches:** `.github/workflows/ci.yml`. Use the `release-ci` subagent.

### [ ] 8.9 Release packaging

- [ ] Second build id in `.goreleaser.yaml`, `goos: [darwin]`,
      `goarch: [amd64, arm64]`, same `tray` tag, same `-s -w -X
      main.version`, **without** `-H=windowsgui` (that flag is rejected by
      the darwin linker). Keep `CGO_ENABLED=0`.
- [ ] `universal_binaries:` with `replace: true` so one
      `callmqtt_<version>_darwin_all.zip` ships instead of two; users
      should not have to know whether they have Apple silicon.
- [ ] Archive as `.zip` for darwin too (keeps one README sentence for
      both platforms; `zip` preserves the executable bit). Same `files:`
      list as the Windows archive.
- [ ] **Bare binary, not a `.app` bundle.** This is the Requirements
      section's "single file, no installer". A bare Mach-O works from a
      terminal and from a LaunchAgent, and systray sets the accessory
      activation policy so there is no Dock icon. A `.app` is only needed
      for Finder double-click, a custom icon and a stable TCC identity —
      all of which come with signing (8.11). Note the trade-off in README.
- [ ] If 8.1 concluded signing is a prerequisite: add a
      `signs:`/`notarize:` step using `APPLE_*` secrets, on a
      `macos-latest` job (notarization needs `xcrun notarytool`, which
      the Windows runner doesn't have). Otherwise `release.yml` stays on
      `windows-latest`; goreleaser cross-compiles darwin from there
      without issue since there is no cgo.

**Done when:** a `v*-rc.N` tag publishes
`callmqtt_<version>_windows_amd64.zip`,
`callmqtt_<version>_windows_arm64.zip`,
`callmqtt_<version>_darwin_all.zip` and `checksums.txt`; the darwin
archive runs on both an Intel and an Apple-silicon Mac via whichever
Gatekeeper path 8.0 established.

**Touches:** `.goreleaser.yaml`, `.github/workflows/release.yml`. Use the
`release-ci` subagent, and do 8.10 in the same change per the CLAUDE.md
release checklist.

### [ ] 8.10 Documentation

This is a change in the *shape* of the release, so CLAUDE.md requires the
README update to land with 8.9, not after it.

- [ ] README badge (`Windows 10 / 11` → `Windows 10 / 11 · macOS 13+`;
      pick the floor from what `CGRequestScreenCaptureAccess` and the
      goffi backend actually need, and state it once).
- [ ] README **Installation**: darwin archive name, the documented
      install path from 8.7, the Gatekeeper path — on macOS 15+ it is
      Settings → Privacy & Security → "Open Anyway"; the older
      right-click → Open no longer works and must not be documented as
      if it does — plus the `xattr -d com.apple.quarantine` command as
      the terminal alternative, and `Go 1.27+` build-from-source lines
      for zsh.
- [ ] README **Quick Start**: config path on macOS, first-launch Screen
      Recording prompt and what happens if it is declined.
- [ ] README **Privacy**: one paragraph on the Screen Recording
      permission — why it is needed (window titles are gated behind it),
      what it is *not* used for (no pixels are ever read, no
      Accessibility access is requested), and that titles are never sent
      to the broker or logged above Debug.
- [ ] README **FAQ**: "titles are empty on macOS" → permission; "the
      permission was granted but stopped working" → the binary moved or
      was upgraded (unsigned TCC grants are path+hash keyed); "developer
      cannot be verified" → Gatekeeper; "mic shows in-use but state stays
      inactive" → the 8.1 attribution rule; "Slack huddles aren't
      detected" if 8.4 found that gap.
- [ ] README **Running on a managed work Mac** (new subsection or FAQ
      cluster): the four obstacles in plain words, the 8.0 canary steps
      the user can run themselves, and "ask IT first" stated once
      without moralising.
- [ ] **Home Assistant Integration**: no change expected — the MQTT
      contract is OS-independent. Verify and say so in the PR.
- [ ] `SECURITY.md` threat model (6.3): add the macOS-specific
      assumptions — TCC grants are per-path+hash when unsigned, the
      LaunchAgent plist is user-writable — and a short **"For IT
      reviewers"** paragraph: exactly which APIs are called
      (`sysctl kern.proc.all`, `CGWindowListCopyWindowInfo`, CoreAudio /
      CoreMediaIO "running somewhere" queries), what leaves the machine
      (one boolean + heartbeat over MQTT, only on `allowed_networks`),
      what never does (titles, process lists), and where the build comes
      from (tagged GitHub release, reproducible via goreleaser). This is
      what turns an EDR alert into an allow-list entry.
- [ ] `CLAUDE.md` release checklist: the asset list now has three
      archives; the "cross-compile darwin" note in `ci.yml` is gone.
- [ ] Run `go run ./.claude/skills/readme-standards/scripts/validate_readme.go --strict`.

**Done when:** the validator passes and a reader who owns only a Mac can
get from the README to `active` in Home Assistant without asking a
question the README already should have answered — including the
question "will this get me in trouble at work".

**Touches:** `README.md`, `SECURITY.md`, `CLAUDE.md`. Use the
`release-ci` subagent.

### [ ] 8.11 Follow-ups, explicitly out of scope for the first darwin release

Listed so they aren't re-argued on every PR. The first one is promoted
into the phase itself if 8.0/8.1 say so.

- [ ] Developer ID signing + notarization in `release.yml` (needs an Apple
      Developer account and secrets; makes TCC grants survive upgrades and
      is the only fix for MDM-disabled Gatekeeper overrides). Even
      notarized, IT may still need to allow-list the Team ID in the PPPC
      or EDR console.
- [ ] `.app` bundle with `Info.plist` (`LSUIElement`, custom icon),
      probably via goreleaser's `dmg`/custom publisher — only worth it
      with signing.
- [ ] Homebrew cask (`brew install --cask callmqtt`) — needs a stable,
      signed artifact first.
- [ ] Per-process mic/camera attribution if Apple ever exposes it, or
      if the unattributed heuristic from 8.1 proves too weak in practice.
- [ ] Accessibility-based detection for apps whose call state isn't in
      any window title — deliberately excluded because it doubles the
      permission ask on a managed machine.
- [ ] Native NSAlert broker dialog through ffi, replacing the "open the
      config file" fallback.
- [ ] `SCNetworkInterfaceGetInterfaceType` classification, shared with the
      Windows `GetAdaptersAddresses` follow-up from 6.2.
- [ ] Linux: 8.2 makes it a matter of adding `platform/linux/` (X11 /
      Wayland titles are a much bigger problem than darwin's); not planned.

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
      (Phases 0–7 don't change the release shape; Phase 8 does — see 8.9/8.10).
