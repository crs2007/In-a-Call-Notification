package supervisor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/mqtt"
	"github.com/crs2007/callmqtt/internal/network"
)

// --- fakes -----------------------------------------------------------------

type fakeDetector struct{ app string }

func (f *fakeDetector) App() string { return f.app }
func (f *fakeDetector) Detect(context.Context) model.DetectionResult {
	return model.DetectionResult{App: f.app, State: model.StateInactive}
}

type fakeChecker struct{}

func (fakeChecker) Current(context.Context) ([]network.Info, error) {
	return []network.Info{{Connected: true, LocalIP: netip.MustParseAddr("192.168.1.5")}}, nil
}

// Capabilities mirrors LocalChecker's real behaviour: only subnet matching
// is ever populated.
func (fakeChecker) Capabilities() network.Capabilities {
	return network.Capabilities{CIDR: true}
}

// fakePublisher tracks whether it was ever closed and the ctx it was built
// with, so tests can assert the old generation actually let go of its
// broker connection and that its connection ctx was independent of the
// caller's ctx. events, when set, is a log shared across every instance one
// test's NewPublisher factory creates, recording "built"/"closed" in the
// order they happened — the thing Reload must get right in 1.2.
type fakePublisher struct {
	ctx    context.Context
	events *[]string
	closed chan struct{}

	// closeDelay, when set, makes Close block for this long before
	// completing — unless ctx is cancelled first, which it respects the same
	// way mqtt.Client.Close's own boundedPublishContext does. Used by the 7.8
	// shutdown-bound test to simulate a slow-but-well-behaved publisher.
	closeDelay time.Duration

	// stateSource records whatever build passed to SetStateSource, so a test
	// can call it directly and assert on what the engine actually wires up —
	// the 7.1 repro.
	stateSource func() (mqtt.Payload, bool)
}

func newFakePublisher(ctx context.Context, events *[]string) *fakePublisher {
	if events != nil {
		*events = append(*events, "built")
	}
	return &fakePublisher{ctx: ctx, events: events, closed: make(chan struct{})}
}

func (f *fakePublisher) PublishState(context.Context, mqtt.Payload) error { return nil }
func (f *fakePublisher) SetStateSource(fn func() (mqtt.Payload, bool))    { f.stateSource = fn }
func (f *fakePublisher) Close(ctx context.Context) error {
	if f.closeDelay > 0 {
		select {
		case <-time.After(f.closeDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.events != nil {
		*f.events = append(*f.events, "closed")
	}
	close(f.closed)
	return nil
}
func (f *fakePublisher) Connected() bool { return true }

// ctxDone reports whether the ctx this publisher was built with has been
// cancelled.
func (f *fakePublisher) ctxDone() bool { return f.ctx.Err() != nil }

func testConfig(t *testing.T, appName string) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.MQTT.Host = "broker.invalid"
	cfg.AllowedNetworks = []config.NetworkRule{{Name: "home", CIDRs: []string{"192.168.1.0/24"}}}
	cfg.Poll.DetectSeconds = 1
	cfg.Detectors = map[string]config.Detector{appName: {Enabled: true}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func fakeDetectorsFactory() func(*config.Config) ([]model.Detector, error) {
	return func(cfg *config.Config) ([]model.Detector, error) {
		names := cfg.EnabledDetectors()
		return []model.Detector{&fakeDetector{app: names[0]}}, nil
	}
}

// --- tests -------------------------------------------------------------

// TestReloadStopsOldBeforeBuildingNew is the 1.2 repro: the old generation's
// publisher must be closed before the new one is built, so the two never
// briefly share one ClientID against the broker.
func TestReloadStopsOldBeforeBuildingNew(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []string
	var built []*fakePublisher
	sup := New(Options{
		Logger:       testLogger(),
		Version:      "test",
		Checker:      fakeChecker{},
		NewDetectors: fakeDetectorsFactory(),
		NewPublisher: func(ctx context.Context, _ *config.Config, _ *slog.Logger, _ string) (Publisher, error) {
			p := newFakePublisher(ctx, &events)
			built = append(built, p)
			return p, nil
		},
	})

	if err := sup.Start(ctx, testConfig(t, "teams")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give the first generation's Run loop a moment to actually start before
	// reloading, so the test exercises a live goroutine, not a race at boot.
	time.Sleep(20 * time.Millisecond)

	if err := sup.Reload(ctx, testConfig(t, "zoom")); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	want := []string{"built", "closed", "built"}
	if len(events) != len(want) {
		t.Fatalf("event log = %v, want %v", events, want)
	}
	for i, ev := range want {
		if events[i] != ev {
			t.Fatalf("event log = %v, want %v (old must close before new is built)", events, want)
		}
	}

	if len(built) != 2 {
		t.Fatalf("expected 2 publishers built, got %d", len(built))
	}
	select {
	case <-built[1].closed:
		t.Fatal("new generation's publisher was closed too — Reload stopped the wrong one")
	default:
	}

	if got := sup.Config(); got == nil || got.Detectors["zoom"].Enabled != true {
		t.Fatalf("Config() did not return the reloaded config")
	}

	if err := sup.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-built[1].closed:
	case <-time.After(2 * time.Second):
		t.Fatal("current generation's publisher was never closed by Close")
	}
}

// TestBuildWiresEngineStateSource is the 7.1 repro: build must wire the
// publisher's SetStateSource to the freshly built engine's CurrentPayload, so
// a broker reconnect can republish current state rather than only "online"
// and the discovery config. Before the engine's first evaluate(), that source
// must report ok == false; after one, it must report the state that
// evaluate() actually computed.
func TestBuildWiresEngineStateSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var built []*fakePublisher
	sup := New(Options{
		Logger:       testLogger(),
		Version:      "test",
		Checker:      fakeChecker{},
		NewDetectors: fakeDetectorsFactory(),
		NewPublisher: func(ctx context.Context, _ *config.Config, _ *slog.Logger, _ string) (Publisher, error) {
			p := newFakePublisher(ctx, nil)
			built = append(built, p)
			return p, nil
		},
	})

	if err := sup.Start(ctx, testConfig(t, "teams")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sup.Close(ctx) }()

	if len(built) != 1 {
		t.Fatalf("expected 1 publisher built, got %d", len(built))
	}
	if built[0].stateSource == nil {
		t.Fatal("build did not call SetStateSource on the publisher")
	}

	// Run's own first poll races this goroutine, so wait for it rather than
	// asserting immediately: ok must eventually become true, and once it
	// does the state must be whatever evaluate() actually computed, never a
	// stale zero value. The very first poll reports "unknown" — the state
	// machine's exit debounce hasn't yet had time to confirm "inactive" — the
	// same "unknown" the startup announcement in engine.go documents.
	deadline := time.Now().Add(2 * time.Second)
	for {
		payload, ok := built[0].stateSource()
		if ok {
			if payload.State != string(model.StateUnknown) {
				t.Errorf("stateSource() state = %q, want %q (first poll, before the exit debounce confirms inactive)", payload.State, model.StateUnknown)
			}
			if payload.Device == "" {
				t.Error("stateSource() payload has no device id")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stateSource() never reported ok == true after Start's first evaluate()")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestPublisherCtxOutlivesCallersCtx is the 1.1 repro: the ctx a caller
// passes to Reload (e.g. the tray's 15s "apply this setting" timeout) must
// not bound the publisher's connection — only Close should end it.
func TestPublisherCtxOutlivesCallersCtx(t *testing.T) {
	var built []*fakePublisher
	sup := New(Options{
		Logger:       testLogger(),
		Version:      "test",
		Checker:      fakeChecker{},
		NewDetectors: fakeDetectorsFactory(),
		NewPublisher: func(ctx context.Context, _ *config.Config, _ *slog.Logger, _ string) (Publisher, error) {
			p := newFakePublisher(ctx, nil)
			built = append(built, p)
			return p, nil
		},
	})

	startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStart()
	if err := sup.Start(startCtx, testConfig(t, "teams")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	reloadCtx, cancelReload := context.WithCancel(context.Background())
	if err := sup.Reload(reloadCtx, testConfig(t, "zoom")); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	cancelReload()

	live := built[len(built)-1]
	time.Sleep(20 * time.Millisecond)
	if live.ctxDone() {
		t.Fatal("live generation's publisher ctx was cancelled along with the caller's Reload ctx")
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClose()
	if err := sup.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !live.ctxDone() {
		t.Fatal("publisher ctx was not cancelled by Close (connection leak)")
	}
}

func TestReloadLeavesRunningGenerationOnDetectorFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	boom := errors.New("boom")
	failNext := false
	var built []*fakePublisher
	sup := New(Options{
		Logger:  testLogger(),
		Version: "test",
		Checker: fakeChecker{},
		NewDetectors: func(cfg *config.Config) ([]model.Detector, error) {
			if failNext {
				return nil, boom
			}
			names := cfg.EnabledDetectors()
			return []model.Detector{&fakeDetector{app: names[0]}}, nil
		},
		NewPublisher: func(ctx context.Context, _ *config.Config, _ *slog.Logger, _ string) (Publisher, error) {
			p := newFakePublisher(ctx, nil)
			built = append(built, p)
			return p, nil
		},
	})

	cfg1 := testConfig(t, "teams")
	if err := sup.Start(ctx, cfg1); err != nil {
		t.Fatalf("Start: %v", err)
	}

	failNext = true
	if err := sup.Reload(ctx, testConfig(t, "zoom")); err == nil {
		t.Fatal("expected Reload to fail")
	}

	select {
	case <-built[0].closed:
		t.Fatal("the running generation was stopped despite the reload failing validation")
	default:
	}
	if got := sup.Config(); got != cfg1 {
		t.Fatal("Config() changed despite the reload failing validation — old generation should still be live")
	}

	if err := sup.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestReloadWithNoLiveGenerationOnPublisherFailure covers the accepted
// tradeoff in 1.2: since the old generation is stopped before the new
// publisher is built, a (rare) publisher build failure leaves no live
// generation at all, rather than preserving the old one.
func TestReloadWithNoLiveGenerationOnPublisherFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	boom := errors.New("boom")
	failNext := false
	var built []*fakePublisher
	sup := New(Options{
		Logger:       testLogger(),
		Version:      "test",
		Checker:      fakeChecker{},
		NewDetectors: fakeDetectorsFactory(),
		NewPublisher: func(ctx context.Context, _ *config.Config, _ *slog.Logger, _ string) (Publisher, error) {
			if failNext {
				return nil, boom
			}
			p := newFakePublisher(ctx, nil)
			built = append(built, p)
			return p, nil
		},
	})

	if err := sup.Start(ctx, testConfig(t, "teams")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	failNext = true
	if err := sup.Reload(ctx, testConfig(t, "zoom")); err == nil {
		t.Fatal("expected Reload to fail")
	}

	select {
	case <-built[0].closed:
	case <-time.After(2 * time.Second):
		t.Fatal("old generation was not stopped before the failed publisher build")
	}
	if got := sup.Status().State; got != model.StateUnknown {
		t.Fatalf("Status() = %v, want unknown once there is no live generation", got)
	}

	if err := sup.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestStopBoundsTotalShutdownTime is the 7.8 repro. gen.done deliberately
// never closes — standing in for a Run loop that ignores its ctx and keeps
// running, a goroutine leak that is already a bug in its own right — and the
// publisher's Close deliberately takes longer than what's left of the shared
// budget once stop gives up waiting on gen.done. Before 7.8, stop's own wait
// and pub.Close's ctx were independent, so the two delays added up; after
// 7.8 they share one deadline, so pub.Close is called with an
// already-expired ctx and returns immediately instead of blocking for its
// own closeDelay.
//
// stopTimeout is shrunk for the duration of this test so the bound can be
// exercised in milliseconds rather than by actually sleeping for the real
// production timeout.
func TestStopBoundsTotalShutdownTime(t *testing.T) {
	const testStopTimeout = 100 * time.Millisecond
	orig := stopTimeout
	stopTimeout = testStopTimeout
	t.Cleanup(func() { stopTimeout = orig })

	// Longer than testStopTimeout: if stop still gave pub.Close a fresh
	// budget instead of sharing the deadline, this delay would dominate and
	// the assertion below would fail.
	pub := &fakePublisher{
		ctx:        context.Background(),
		closed:     make(chan struct{}),
		closeDelay: 10 * testStopTimeout,
	}
	gen := &generation{
		pub:       pub,
		cancel:    func() {},
		pubCancel: func() {},
		done:      make(chan struct{}), // never closed: simulates a stuck engine
	}

	sup := &Supervisor{log: testLogger()}

	start := time.Now()
	stopped := make(chan struct{})
	go func() {
		sup.stop(context.Background(), gen)
		close(stopped)
	}()

	// Generous upper bound so a genuine regression (delays stacking instead
	// of sharing a deadline) fails the test instead of hanging it.
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not return within the bounded shutdown window (regression: delays are stacking instead of sharing one deadline)")
	}

	if elapsed := time.Since(start); elapsed > 3*testStopTimeout {
		t.Errorf("stop took %v, want at most ~%v (testStopTimeout plus slack, not testStopTimeout+closeDelay)", elapsed, 3*testStopTimeout)
	}
}

func TestStatusBeforeStartIsUnknown(t *testing.T) {
	sup := New(Options{Logger: testLogger()})
	if got := sup.Status().State; got != model.StateUnknown {
		t.Fatalf("Status() before Start = %v, want unknown", got)
	}
}
