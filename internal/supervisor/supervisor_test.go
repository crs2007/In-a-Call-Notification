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
}

func newFakePublisher(ctx context.Context, events *[]string) *fakePublisher {
	if events != nil {
		*events = append(*events, "built")
	}
	return &fakePublisher{ctx: ctx, events: events, closed: make(chan struct{})}
}

func (f *fakePublisher) PublishState(context.Context, mqtt.Payload) error { return nil }
func (f *fakePublisher) Close(context.Context) error {
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

func TestStatusBeforeStartIsUnknown(t *testing.T) {
	sup := New(Options{Logger: testLogger()})
	if got := sup.Status().State; got != model.StateUnknown {
		t.Fatalf("Status() before Start = %v, want unknown", got)
	}
}
