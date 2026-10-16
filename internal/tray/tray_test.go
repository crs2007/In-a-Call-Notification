//go:build tray

package tray

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gogpu/systray"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/engine"
	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/network"
)

func TestIconPNG(t *testing.T) {
	for _, icon := range []trayIconState{
		{color: colorGray},
		{color: colorGreen, brokerConnected: true},
		{color: colorYellow},
		{color: colorRed, brokerConnected: true},
	} {
		data := iconPNG(icon)
		if len(data) == 0 {
			t.Fatalf("empty png for %+v", icon)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode png for %+v: %v", icon, err)
		}
		if img.Bounds().Dx() != 22 || img.Bounds().Dy() != 22 {
			t.Fatalf("expected 22x22, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
		}

		got := color.RGBAModel.Convert(img.At(17, 17)).(color.RGBA)
		want := color.RGBA{R: 239, G: 68, B: 68, A: 255}
		if icon.brokerConnected {
			want = color.RGBA{R: 34, G: 197, B: 94, A: 255}
		}
		if got != want {
			t.Fatalf("badge for %+v = %+v, want %+v", icon, got, want)
		}
	}
}

func TestStatusTooltip(t *testing.T) {
	tests := []struct {
		name      string
		status    engine.Status
		connected bool
		want      string
	}{
		{"active and connected", engine.Status{State: model.StateActive, App: "teams"}, true, "In a Call Notification | On call: yes (Teams) | MQTT: connected"},
		{"active and disconnected", engine.Status{State: model.StateActive, App: "teams"}, false, "In a Call Notification | On call: yes (Teams) | MQTT: disconnected"},
		{"inactive", engine.Status{State: model.StateInactive}, true, "In a Call Notification | On call: not on a call | MQTT: connected"},
		{"paused", engine.Status{State: model.StateActive, Paused: true}, false, "In a Call Notification | On call: detection paused | MQTT: disconnected"},
		{"starting", engine.Status{State: model.StateUnknown}, false, "In a Call Notification | On call: starting | MQTT: disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusTooltip(tt.status, tt.connected); got != tt.want {
				t.Fatalf("statusTooltip() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAllowNetLabelAndDisabled(t *testing.T) {
	home := netip.MustParsePrefix("192.168.1.0/24")
	tests := []struct {
		name         string
		status       engine.Status
		wantLabel    string
		wantDisabled bool
	}{
		{
			name:         "no candidate network disables the item",
			status:       engine.Status{},
			wantLabel:    "Allow current network",
			wantDisabled: true,
		},
		{
			name:         "candidate matching a rule already, item is inert",
			status:       engine.Status{Network: network.Info{Prefix: home}, NetworkRule: "Home", Allowed: true},
			wantLabel:    "Allow current network (192.168.1.0/24)",
			wantDisabled: true,
		},
		{
			// This is the issue #3 repro: no rule matches, but the engine
			// still reports the connected candidate so the item is
			// enabled and names the subnet the user would need to add.
			name:         "no rule matches a connected candidate, item is enabled with its prefix",
			status:       engine.Status{Network: network.Info{Prefix: home}, NetworkRule: "", Allowed: false},
			wantLabel:    "Allow current network (192.168.1.0/24)",
			wantDisabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowNetLabel(tt.status); got != tt.wantLabel {
				t.Errorf("allowNetLabel() = %q, want %q", got, tt.wantLabel)
			}
			if got := allowNetDisabled(tt.status); got != tt.wantDisabled {
				t.Errorf("allowNetDisabled() = %v, want %v", got, tt.wantDisabled)
			}
		})
	}
}

func TestNetworkLabel(t *testing.T) {
	tests := []struct {
		name   string
		status engine.Status
		want   string
	}{
		{"no rule", engine.Status{}, "Network: not allowed — not publishing"},
		{"publishing", engine.Status{NetworkRule: "Home", Allowed: true}, "Network: Home — publishing"},
		{"wide rule publishing", engine.Status{NetworkRule: "Home", NetworkRuleWide: true, Allowed: true}, "Network: Home (wide rule) — publishing"},
		{"wide rule not publishing", engine.Status{NetworkRule: "Home", NetworkRuleWide: true}, "Network: Home (wide rule) — not publishing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := networkLabel(tt.status); got != tt.want {
				t.Errorf("networkLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// fakeSupervisor is a minimal supervisorAPI, so tests never touch a real
// engine or broker connection. Status()'s State flips on every call and
// BrokerConnected() flips on every call, so refresh() computes a genuinely
// different icon most of the time — the point being to make a.lastIcon get
// written often, not just read, while it's under concurrent pressure.
type fakeSupervisor struct {
	mu        sync.Mutex
	cfg       *config.Config
	paused    bool
	connected bool
	tick      int
	reloads   int
}

func (f *fakeSupervisor) Config() *config.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}

func (f *fakeSupervisor) Status() engine.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tick++
	state := model.StateInactive
	if f.tick%2 == 0 {
		state = model.StateActive
	}
	return engine.Status{State: state, App: "teams", Allowed: true}
}

func (f *fakeSupervisor) Reload(_ context.Context, cfg *config.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = cfg
	f.reloads++
	return nil
}

func (f *fakeSupervisor) SetPaused(paused bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = paused
}

func (f *fakeSupervisor) Paused() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused
}

func (f *fakeSupervisor) BrokerConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = !f.connected
	return f.connected
}

// newTestApp builds an app the way Run does, minus the parts that would
// actually show a tray icon or pump the OS message loop — systray.New()'s
// message-only window and menu construction work fine without either.
func newTestApp(t *testing.T, sup supervisorAPI, cfgPath string, pollInterval time.Duration) *app {
	t.Helper()
	return newTestAppWith(t, Options{
		Supervisor:   sup,
		ConfigPath:   cfgPath,
		PollInterval: pollInterval,
	})
}

// newTestAppWith is newTestApp for tests that need other Options set.
//
// On macOS it skips: AppKit only lets the main thread create a status item,
// and menu updates are handed to the main thread's run loop and waited on,
// but go test runs every test on its own goroutine with no run loop. The
// tray's macOS behaviour is covered by running the app itself instead.
func newTestAppWith(t *testing.T, opts Options) *app {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip("systray needs the AppKit main thread and run loop, which go test does not provide on macOS")
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	a := &app{opts: opts, tray: systray.New(), refreshNow: make(chan struct{}, 1)}
	a.buildMenu()
	return a
}

// fixedStatusSupervisor is a supervisorAPI stub that always reports the same
// Status, for tests that check what a.refresh() does with one specific
// snapshot rather than fakeSupervisor's ticking state.
type fixedStatusSupervisor struct {
	status engine.Status
}

func (f fixedStatusSupervisor) Config() *config.Config                       { return &config.Config{} }
func (f fixedStatusSupervisor) Status() engine.Status                        { return f.status }
func (f fixedStatusSupervisor) Reload(context.Context, *config.Config) error { return nil }
func (f fixedStatusSupervisor) SetPaused(bool)                               {}
func (f fixedStatusSupervisor) Paused() bool                                 { return false }
func (f fixedStatusSupervisor) BrokerConnected() bool                        { return false }

// TestRefreshEnablesAllowCurrentNetworkForUnmatchedCandidate is the issue #3
// repro: on a network that matches no allow-list rule, the tray's "Allow
// current network" item used to be disabled precisely in the one situation
// it exists for, because engine.Status().Network was the zero value whenever
// nothing matched. It must now be enabled and name the candidate's subnet.
func TestRefreshEnablesAllowCurrentNetworkForUnmatchedCandidate(t *testing.T) {
	prefix := netip.MustParsePrefix("192.168.68.0/24")
	sup := fixedStatusSupervisor{status: engine.Status{
		Network:     network.Info{Interface: "Wi-Fi", Prefix: prefix},
		NetworkRule: "",
		Allowed:     false,
	}}
	a := newTestApp(t, sup, "", time.Second)

	a.refresh()

	if a.allowNetItem.IsDisabled() {
		t.Fatal("Allow current network should be enabled when a connected candidate matches no rule")
	}
}

// TestConcurrentApplyChangesBothPersist is the issue #4.2 repro: toggling two
// different detectors back-to-back must leave both changes in the saved
// config and the reloaded engine, not just whichever click's applyChange
// happened to read Supervisor.Config() last. Before applyMu serialised
// applyChange, the second goroutine derived its settings from the same
// pre-toggle config the first one started from, so its Save silently wrote
// back the first toggle's field as if it had never changed.
func TestConcurrentApplyChangesBothPersist(t *testing.T) {
	t.Setenv("CALLMQTT_MQTT_PASSWORD", "unused-test-password")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, config.Example, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Detectors["teams"].Enabled || !cfg.Detectors["zoom"].Enabled {
		t.Fatal("test config must start with both teams and zoom enabled")
	}

	sup := &fakeSupervisor{cfg: cfg}
	a := newTestApp(t, sup, cfgPath, time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.toggleDetector("teams") }()
	go func() { defer wg.Done(); a.toggleDetector("zoom") }()
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for {
		sup.mu.Lock()
		reloads := sup.reloads
		sup.mu.Unlock()
		if reloads >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 2 expected reloads landed", reloads)
		}
		time.Sleep(5 * time.Millisecond)
	}

	final := sup.Config()
	if final.Detectors["teams"].Enabled {
		t.Error("teams should have been toggled off, but the reloaded config still has it enabled (lost update)")
	}
	if final.Detectors["zoom"].Enabled {
		t.Error("zoom should have been toggled off, but the reloaded config still has it enabled (lost update)")
	}

	onDisk, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if onDisk.Detectors["teams"].Enabled {
		t.Error("saved config still has teams enabled (lost update)")
	}
	if onDisk.Detectors["zoom"].Enabled {
		t.Error("saved config still has zoom enabled (lost update)")
	}
}

// TestRefreshIsRaceFreeUnderConcurrentApplyChangeAndTogglePause is the repro
// for TODO.md 3.4: before the fix, applyChange and togglePause each called
// a.refresh() directly from their own goroutines, racing with refreshLoop's
// goroutine over a.lastIcon. Run with -race: it must fail on the old code
// (direct a.refresh() calls) and pass once applyChange/togglePause only ever
// request a refresh through refreshNow.
func TestRefreshIsRaceFreeUnderConcurrentApplyChangeAndTogglePause(t *testing.T) {
	// applyChange reloads the config on every toggle; set this so the
	// package-level "env var referenced but not set" warning doesn't fire on
	// every one of them.
	t.Setenv("CALLMQTT_MQTT_PASSWORD", "unused-test-password")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, config.Example, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	a := newTestApp(t, &fakeSupervisor{cfg: cfg}, cfgPath, time.Millisecond)

	stop := make(chan struct{})
	go a.refreshLoop(stop)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				a.toggleDetector("teams")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				a.togglePause()
			}
		}()
	}
	wg.Wait()

	// toggleDetector's applyChange runs its Save/Load/Reload/refresh on a
	// spawned goroutine; give the slowest of those a chance to land — and to
	// race against refreshLoop, still running below — before stop closes it.
	time.Sleep(200 * time.Millisecond)
	close(stop)
}

// TestRefreshPermissionThrottlesCheckAndTracksGrant pins the Permission
// item's contract: Check runs on the first refresh, not again until
// permissionCheckEvery has passed, and a grant turns the action into a
// disabled status line.
func TestRefreshPermissionThrottlesCheckAndTracksGrant(t *testing.T) {
	var checks int
	granted := false
	a := newTestAppWith(t, Options{
		Supervisor: fixedStatusSupervisor{},
		Permission: &Permission{
			Missing: "Allow window titles…",
			Granted: "Window titles: allowed",
			Check:   func() bool { checks++; return granted },
			Request: func() {},
		},
	})

	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	a.refreshPermission(start)
	if checks != 1 || a.permItem.IsDisabled() {
		t.Fatalf("first refresh: checks=%d disabled=%v, want 1 and enabled", checks, a.permItem.IsDisabled())
	}

	granted = true
	a.refreshPermission(start.Add(permissionCheckEvery / 2))
	if checks != 1 {
		t.Fatalf("refresh inside the throttle window called Check again (%d calls)", checks)
	}

	a.refreshPermission(start.Add(permissionCheckEvery))
	if checks != 2 || !a.permItem.IsDisabled() {
		t.Fatalf("after window: checks=%d disabled=%v, want 2 and disabled", checks, a.permItem.IsDisabled())
	}
}

func TestNoPermissionMeansNoItem(t *testing.T) {
	a := newTestApp(t, fixedStatusSupervisor{}, "", time.Second)
	if a.permItem != nil {
		t.Fatal("a nil Options.Permission must not add a menu item")
	}
	a.refreshPermission(time.Now()) // must not panic
}

func TestPermissionLabel(t *testing.T) {
	p := &Permission{Missing: "Allow…", Granted: "Allowed"}
	if got := permissionLabel(p, false); got != "Allow…" {
		t.Errorf("missing: got %q", got)
	}
	if got := permissionLabel(p, true); got != "Allowed" {
		t.Errorf("granted: got %q", got)
	}
}
