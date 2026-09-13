package engine

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

// fakeDetector reports whatever the test tells it to, so the engine can be
// driven through scenarios that would otherwise need a real meeting.
type fakeDetector struct {
	app        string
	state      model.CallState
	confidence float64
}

func (f *fakeDetector) App() string { return f.app }

func (f *fakeDetector) Detect(context.Context) model.DetectionResult {
	return model.DetectionResult{App: f.app, State: f.state, Confidence: f.confidence}
}

type fakeChecker struct {
	info network.Info
	err  error
}

func (f *fakeChecker) Current(context.Context) (network.Info, error) {
	return f.info, f.err
}

type fakePublisher struct {
	sent []mqtt.Payload
	err  error
}

func (f *fakePublisher) PublishState(_ context.Context, p mqtt.Payload) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, p)
	return nil
}

func (f *fakePublisher) states() []string {
	out := make([]string, len(f.sent))
	for i, p := range f.sent {
		out[i] = p.State
	}
	return out
}

// --- harness ---------------------------------------------------------------

func homeNetwork() network.Info {
	return network.Info{
		Connected: true,
		Interface: "Ethernet",
		LocalIP:   netip.MustParseAddr("192.168.1.42"),
	}
}

func hotspot() network.Info {
	return network.Info{
		Connected: true,
		Interface: "Wi-Fi",
		SSID:      "Sharon iPhone",
		LocalIP:   netip.MustParseAddr("172.20.10.2"),
	}
}

type harness struct {
	engine    *Engine
	detector  *fakeDetector
	checker   *fakeChecker
	publisher *fakePublisher
	start     time.Time
	at        time.Duration // clock cursor, so successive runs never go backwards
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	cfg, err := config.Parse([]byte(`
allowed_networks:
  - name: Home
    cidrs: ["192.168.1.0/24"]
mqtt:
  host: 192.168.1.10
`))
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	h := &harness{
		detector:  &fakeDetector{app: "teams", state: model.StateInactive},
		checker:   &fakeChecker{info: homeNetwork()},
		publisher: &fakePublisher{},
		start:     time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC),
	}

	h.engine, err = New(Options{
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Detectors: []model.Detector{h.detector},
		Checker:   h.checker,
		Publisher: h.publisher,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

// tick runs one poll cycle at the given offset from the start.
func (h *harness) tick(at time.Duration) {
	h.at = at + pollInterval
	h.engine.Evaluate(context.Background(), h.start.Add(at))
}

// runUntil polls every 2 seconds from wherever the clock currently is through
// the given offset, inclusive.
func (h *harness) runUntil(until time.Duration) {
	for at := h.at; at <= until; at += pollInterval {
		h.tick(at)
	}
}

const pollInterval = 2 * time.Second

// --- the acceptance table --------------------------------------------------

// S1 and S2: the bulb follows a real call on and off.
func TestJoinAndLeaveACall(t *testing.T) {
	h := newHarness(t)

	h.runUntil(10 * time.Second) // settles to inactive
	h.detector.state, h.detector.confidence = model.StateActive, 0.85
	h.runUntil(30 * time.Second)
	h.detector.state, h.detector.confidence = model.StateInactive, 0.20
	h.runUntil(60 * time.Second)

	// The leading "unknown" is the startup announcement that clears any stale
	// retained state from a previous run.
	want := []string{"unknown", "inactive", "active", "inactive"}
	got := h.publisher.states()
	if len(got) != len(want) {
		t.Fatalf("published %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("publish %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// S3, S7 and S8 are the false-positive guards, and they are release-blocking.
// A detector that never reports active must produce no active publish at all.
func TestNeverPublishesActiveWithoutADetection(t *testing.T) {
	scenarios := []struct {
		name       string
		confidence float64
	}{
		{"zoom open all day, never joined (S3)", 0.20},
		{"music playing, mic held by a media app (S7)", 0.15},
		{"teams sitting on a chat window (S8)", 0.20},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			h := newHarness(t)
			h.detector.state, h.detector.confidence = model.StateInactive, s.confidence

			h.runUntil(8 * time.Hour)

			for i, p := range h.publisher.sent {
				if p.State == "active" {
					t.Fatalf("publish %d reported active with no detection: %+v", i, p)
				}
			}
		})
	}
}

// S4: muting or toggling a camera must not reach the broker at all.
func TestBriefDropoutMidCallIsNotPublished(t *testing.T) {
	h := newHarness(t)

	h.runUntil(10 * time.Second)
	h.detector.state = model.StateActive
	h.runUntil(30 * time.Second)

	before := len(h.publisher.sent)

	// Two seconds of "not in a call" while muting, then back.
	h.detector.state = model.StateInactive
	h.tick(32 * time.Second)
	h.detector.state = model.StateActive
	h.tick(34 * time.Second)
	h.tick(36 * time.Second)

	if len(h.publisher.sent) != before {
		t.Errorf("a brief dropout produced %d extra publishes: %v",
			len(h.publisher.sent)-before, h.publisher.states()[before:])
	}
	if h.engine.Status().State != model.StateActive {
		t.Errorf("state = %q, want the call to still be active", h.engine.Status().State)
	}
}

// S5: leaving the allowed network mid-call stops publishing entirely. Home
// Assistant's expire_after then resolves the entity without our help.
func TestLeavingTheAllowedNetworkStopsPublishing(t *testing.T) {
	h := newHarness(t)

	h.runUntil(10 * time.Second)
	h.detector.state = model.StateActive
	h.runUntil(30 * time.Second)

	if h.engine.Status().State != model.StateActive {
		t.Fatal("expected an active call before switching networks")
	}
	before := len(h.publisher.sent)

	h.checker.info = hotspot()
	h.runUntil(10 * time.Minute)

	if len(h.publisher.sent) != before {
		t.Errorf("published %d messages from a disallowed network: %v",
			len(h.publisher.sent)-before, h.publisher.states()[before:])
	}
}

// Rejoining republishes at once rather than waiting for the next heartbeat.
func TestRejoiningTheAllowedNetworkRepublishes(t *testing.T) {
	h := newHarness(t)

	h.runUntil(10 * time.Second)
	h.detector.state = model.StateActive
	h.runUntil(30 * time.Second)

	h.checker.info = hotspot()
	h.runUntil(90 * time.Second)
	before := len(h.publisher.sent)

	h.checker.info = homeNetwork()
	h.tick(100 * time.Second)

	if len(h.publisher.sent) <= before {
		t.Fatal("rejoining an allowed network should republish the current state")
	}
	if last := h.publisher.sent[len(h.publisher.sent)-1]; last.State != "active" {
		t.Errorf("republished %q, want the current active state", last.State)
	}
}

// A network that cannot be read denies. Not knowing where the machine is
// connected is not a reason to assume it is somewhere safe.
func TestNetworkErrorSuspendsPublishing(t *testing.T) {
	h := newHarness(t)
	h.checker.err = errors.New("no adapters")
	h.detector.state = model.StateActive

	h.runUntil(5 * time.Minute)

	if len(h.publisher.sent) != 0 {
		t.Errorf("published %v while the network was unreadable", h.publisher.states())
	}
}

// An empty allow-list is a configuration mistake, and it must fail silent
// rather than broadcast from anywhere.
func TestNoAllowedNetworksPublishesNothing(t *testing.T) {
	cfg := config.Defaults()
	cfg.MQTT.Host = "broker"

	publisher := &fakePublisher{}
	e, err := New(Options{
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Detectors: nil,
		Checker:   &fakeChecker{info: homeNetwork()},
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	for at := time.Duration(0); at <= time.Hour; at += 2 * time.Second {
		e.Evaluate(context.Background(), start.Add(at))
	}

	if len(publisher.sent) != 0 {
		t.Errorf("published %d messages with an empty allow-list", len(publisher.sent))
	}
}

// The heartbeat re-asserts state so a silent agent is distinguishable from a
// dead one, but it must not fire every tick.
func TestHeartbeat(t *testing.T) {
	h := newHarness(t)

	// Startup announcement, then the settled inactive state.
	h.runUntil(10 * time.Second)
	first := len(h.publisher.sent)
	if first != 2 {
		t.Fatalf("expected the startup announcement and one state publish, got %v", h.publisher.states())
	}

	// Default heartbeat is 60s. Five minutes of unchanging state should
	// produce roughly five beats, not 150.
	h.runUntil(5*time.Minute + 10*time.Second)

	beats := len(h.publisher.sent) - first
	if beats < 4 || beats > 6 {
		t.Errorf("got %d heartbeats in 5 minutes, want about 5 (publishes: %d)", beats, len(h.publisher.sent))
	}
	for _, p := range h.publisher.sent[1:] {
		if p.State != "inactive" {
			t.Errorf("heartbeat published %q, want the unchanged inactive state", p.State)
		}
	}
}

// A broker that rejects a publish must not be recorded as having received it,
// or the heartbeat would paper over a permanently failing connection.
func TestFailedPublishIsRetried(t *testing.T) {
	h := newHarness(t)
	h.publisher.err = errors.New("broker unreachable")

	h.runUntil(10 * time.Second)
	if len(h.publisher.sent) != 0 {
		t.Fatal("expected no recorded publishes while failing")
	}

	h.publisher.err = nil
	h.tick(12 * time.Second)

	if len(h.publisher.sent) == 0 {
		t.Error("once the broker recovered, the pending state should be published")
	}
}

// An inactive payload must not name the app that was last in a call.
func TestInactivePayloadCarriesNoApp(t *testing.T) {
	h := newHarness(t)

	h.runUntil(10 * time.Second)
	h.detector.state = model.StateActive
	h.runUntil(30 * time.Second)
	h.detector.state = model.StateInactive
	h.runUntil(60 * time.Second)

	last := h.publisher.sent[len(h.publisher.sent)-1]
	if last.State != "inactive" {
		t.Fatalf("expected the final publish to be inactive, got %q", last.State)
	}
	if last.App != "" || len(last.Apps) != 0 {
		t.Errorf("inactive payload named apps %q/%v", last.App, last.Apps)
	}
}

// Two apps in a call at once are both reported rather than one being picked.
func TestTwoActiveAppsAreBothReported(t *testing.T) {
	cfg, err := config.Parse([]byte(`
allowed_networks:
  - name: Home
    cidrs: ["192.168.1.0/24"]
mqtt:
  host: 192.168.1.10
`))
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	publisher := &fakePublisher{}
	e, err := New(Options{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Detectors: []model.Detector{
			&fakeDetector{app: "zoom", state: model.StateActive, confidence: 0.75},
			&fakeDetector{app: "teams", state: model.StateActive, confidence: 0.95},
		},
		Checker:   &fakeChecker{info: homeNetwork()},
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	for at := time.Duration(0); at <= 10*time.Second; at += 2 * time.Second {
		e.Evaluate(context.Background(), start.Add(at))
	}

	if len(publisher.sent) == 0 {
		t.Fatal("expected a publish")
	}
	p := publisher.sent[len(publisher.sent)-1]
	if p.App != "teams" {
		t.Errorf("app = %q, want the most confident app", p.App)
	}
	if len(p.Apps) != 2 {
		t.Errorf("apps = %v, want both active apps reported", p.Apps)
	}
}
