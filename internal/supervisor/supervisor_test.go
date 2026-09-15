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

// fakePublisher tracks whether it was ever closed, so tests can assert the
// old generation actually let go of its broker connection.
type fakePublisher struct {
	closed  chan struct{}
	failNew bool
}

func newFakePublisher() *fakePublisher { return &fakePublisher{closed: make(chan struct{})} }

func (f *fakePublisher) PublishState(context.Context, mqtt.Payload) error { return nil }
func (f *fakePublisher) Close(context.Context) error {
	close(f.closed)
	return nil
}
func (f *fakePublisher) Connected() bool { return true }

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

// --- tests -------------------------------------------------------------

func TestReloadSwapsGenerationAndStopsTheOld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var built []*fakePublisher
	sup := New(Options{
		Logger:  testLogger(),
		Version: "test",
		Checker: fakeChecker{},
		NewDetectors: func(cfg *config.Config) ([]model.Detector, error) {
			names := cfg.EnabledDetectors()
			return []model.Detector{&fakeDetector{app: names[0]}}, nil
		},
		NewPublisher: func(context.Context, *config.Config, *slog.Logger, string) (Publisher, error) {
			p := newFakePublisher()
			built = append(built, p)
			return p, nil
		},
	})

	cfg1 := testConfig(t, "teams")
	if err := sup.Start(ctx, cfg1); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give the first generation's Run loop a moment to actually start before
	// reloading, so the test exercises a live goroutine, not a race at boot.
	time.Sleep(20 * time.Millisecond)

	cfg2 := testConfig(t, "zoom")
	if err := sup.Reload(ctx, cfg2); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if len(built) != 2 {
		t.Fatalf("expected 2 publishers built, got %d", len(built))
	}
	select {
	case <-built[0].closed:
	case <-time.After(2 * time.Second):
		t.Fatal("old generation's publisher was never closed (goroutine/connection leak)")
	}
	select {
	case <-built[1].closed:
		t.Fatal("new generation's publisher was closed too — Reload stopped the wrong one")
	default:
	}

	if got := sup.Config(); got != cfg2 {
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

func TestReloadLeavesRunningGenerationOnBuildFailure(t *testing.T) {
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
			names := cfg.EnabledDetectors()
			return []model.Detector{&fakeDetector{app: names[0]}}, nil
		},
		NewPublisher: func(context.Context, *config.Config, *slog.Logger, string) (Publisher, error) {
			if failNext {
				return nil, boom
			}
			p := newFakePublisher()
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
		t.Fatal("the running generation was stopped despite the reload failing")
	default:
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
