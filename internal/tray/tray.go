//go:build tray

// Package tray is the system tray front end for CallMQTT.
//
// It never decides anything about call state — it reads the engine's status
// through the supervisor and renders it, and every setting it changes goes
// through config.Save followed by supervisor.Reload. That keeps this package
// a thin, swappable view: the engine works identically, and is fully tested,
// with no tray running at all.
package tray

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gogpu/systray"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/engine"
	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/supervisor"
)

// Startup manages whether the agent launches at login. Implementations live
// in platform/. Left nil, the tray simply omits the checkbox rather than
// showing a control that does nothing.
type Startup interface {
	IsEnabled() (bool, error)
	Enable() error
	Disable() error
}

// Dialog collects broker settings from the user and reports whether they
// confirmed. Left nil, "Broker settings…" falls back to opening the config
// file, since there is otherwise no way to change those four fields at all.
type Dialog func(current config.Settings) (config.Settings, bool, error)

// Options configures the tray.
type Options struct {
	Supervisor   *supervisor.Supervisor
	Logger       *slog.Logger
	ConfigPath   string
	LogPath      string
	PollInterval time.Duration // default 2s
	Dialog       Dialog
	Startup      Startup
}

// Run builds the tray icon and menu and blocks pumping the OS message loop
// until Quit is chosen. Call it from main after everything else is wired up.
func Run(opts Options) error {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}

	a := &app{opts: opts, tray: systray.New()}
	a.buildMenu()

	a.tray.SetIcon(iconPNG(colorGray)).
		SetTooltip("CallMQTT").
		SetMenu(a.menu)
	a.tray.Show()

	stop := make(chan struct{})
	go a.refreshLoop(stop)
	defer close(stop)

	return a.tray.Run()
}

type app struct {
	opts Options
	tray *systray.SystemTray
	menu *systray.Menu

	stateItem    *systray.MenuItem
	networkItem  *systray.MenuItem
	brokerItem   *systray.MenuItem
	detectItems  map[string]*systray.MenuItem
	discoverItem *systray.MenuItem
	allowNetItem *systray.MenuItem
	loginItem    *systray.MenuItem
	pauseItem    *systray.MenuItem

	lastIcon iconColor
}

func (a *app) log() *slog.Logger { return a.opts.Logger }

// buildMenu lays out the fixed structure from the plan. Only the labels and
// checked-state change afterwards; the menu itself is built once.
func (a *app) buildMenu() {
	m := systray.NewMenu()

	a.stateItem = m.Add("state", nil)
	a.stateItem.SetDisabled(true)
	a.networkItem = m.Add("network", nil)
	a.networkItem.SetDisabled(true)
	a.brokerItem = m.Add("broker", nil)
	a.brokerItem.SetDisabled(true)
	m.AddSeparator()

	cfg := a.opts.Supervisor.Config()
	names := make([]string, 0, len(cfg.Detectors))
	for name := range cfg.Detectors {
		names = append(names, name)
	}
	sort.Strings(names)

	a.detectItems = make(map[string]*systray.MenuItem, len(names))
	for _, name := range names {
		name := name
		a.detectItems[name] = m.AddCheckbox(title(name), cfg.Detectors[name].Enabled, func() {
			a.toggleDetector(name)
		})
	}

	a.discoverItem = m.AddCheckbox("Home Assistant discovery", cfg.MQTT.Discovery.Enabled, func() {
		a.toggleDiscovery()
	})
	a.allowNetItem = m.Add("Allow current network", func() { a.allowCurrentNetwork() })
	m.AddSeparator()

	m.Add("Broker settings…", func() { a.openBrokerDialog() })
	if a.opts.Startup != nil {
		enabled, err := a.opts.Startup.IsEnabled()
		if err != nil {
			a.log().Warn("check startup state", "error", err)
		}
		a.loginItem = m.AddCheckbox("Start at login", enabled, func() { a.toggleStartup() })
	}
	a.pauseItem = m.Add("Pause detection", func() { a.togglePause() })
	m.AddSeparator()

	m.Add("Open config file", func() { openFile(a.opts.ConfigPath) })
	if a.opts.LogPath != "" {
		m.Add("Open logs", func() { openFile(a.opts.LogPath) })
	}
	// Remove() posts the platform's quit signal, which makes Run() return to
	// the caller — main.go shuts the supervisor down from there, so quitting
	// from the tray still publishes "offline" instead of just vanishing.
	m.Add("Quit", func() { a.tray.Remove() })

	a.menu = m
}

// refreshLoop keeps the status lines, icon and pause label in step with the
// engine, which changes on its own poll cadence independent of tray clicks.
func (a *app) refreshLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(a.opts.PollInterval)
	defer ticker.Stop()

	a.refresh()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			a.refresh()
		}
	}
}

func (a *app) refresh() {
	status := a.opts.Supervisor.Status()

	a.stateItem.SetLabel(stateLabel(status))
	a.networkItem.SetLabel(networkLabel(status))
	a.brokerItem.SetLabel(brokerLabel(a.opts.Supervisor.BrokerConnected()))

	if status.Network.Prefix.IsValid() {
		a.allowNetItem.SetLabel(fmt.Sprintf("Allow current network (%s)", status.Network.Prefix))
		// Already matching a rule is the common "nothing to do" case, not an
		// error, so the item just goes inert rather than showing a message.
		a.allowNetItem.SetDisabled(status.NetworkRule != "")
	} else {
		a.allowNetItem.SetLabel("Allow current network")
		a.allowNetItem.SetDisabled(true)
	}

	if status.Paused {
		a.pauseItem.SetLabel("Resume detection")
	} else {
		a.pauseItem.SetLabel("Pause detection")
	}

	c := iconFor(status, a.opts.Supervisor.BrokerConnected())
	if c != a.lastIcon {
		a.tray.SetIcon(iconPNG(c))
		a.lastIcon = c
	}
}

func stateLabel(s engine.Status) string {
	if s.Paused {
		return "● Detection paused"
	}
	switch s.State {
	case model.StateActive:
		app := s.App
		if app == "" {
			app = strings.Join(s.Apps, ", ")
		}
		return fmt.Sprintf("● On call — %s", app)
	case model.StateInactive:
		return "● Not on a call"
	default:
		return "● Starting…"
	}
}

func networkLabel(s engine.Status) string {
	if s.NetworkRule == "" {
		return "Network: not allowed — not publishing"
	}
	if s.Allowed {
		return fmt.Sprintf("Network: %s — publishing", s.NetworkRule)
	}
	return fmt.Sprintf("Network: %s — not publishing", s.NetworkRule)
}

func brokerLabel(connected bool) string {
	if connected {
		return "Broker: connected"
	}
	return "Broker: disconnected"
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- actions ----------------------------------------------------------------

// applyChange reads the current settings, mutates them, saves, reloads the
// supervisor, and on failure reverts the visible checkbox so the tray never
// claims a change took effect when it didn't. It runs off the systray
// callback goroutine because a reload may dial the broker, which must not
// block the message loop.
func (a *app) applyChange(mutate func(*config.Settings), item *systray.MenuItem, revert bool) {
	go func() {
		cfg := a.opts.Supervisor.Config()
		settings := cfg.Settings()
		mutate(&settings)

		if err := config.Save(a.opts.ConfigPath, settings); err != nil {
			a.log().Error("save settings", "error", err)
			if item != nil {
				item.SetChecked(revert)
			}
			return
		}
		newCfg, err := config.Load(a.opts.ConfigPath)
		if err != nil {
			a.log().Error("reload config after save", "error", err)
			if item != nil {
				item.SetChecked(revert)
			}
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.opts.Supervisor.Reload(ctx, newCfg); err != nil {
			a.log().Error("apply settings", "error", err)
			if item != nil {
				item.SetChecked(revert)
			}
			return
		}
		a.refresh()
	}()
}

func (a *app) toggleDetector(name string) {
	item := a.detectItems[name]
	was := item.IsChecked()
	next := !was
	item.SetChecked(next)
	a.applyChange(func(s *config.Settings) { s.Detectors[name] = next }, item, was)
}

func (a *app) toggleDiscovery() {
	was := a.discoverItem.IsChecked()
	next := !was
	a.discoverItem.SetChecked(next)
	a.applyChange(func(s *config.Settings) { s.Discovery = next }, a.discoverItem, was)
}

func (a *app) toggleStartup() {
	if a.opts.Startup == nil {
		return
	}
	was := a.loginItem.IsChecked()
	next := !was
	a.loginItem.SetChecked(next)

	go func() {
		var err error
		if next {
			err = a.opts.Startup.Enable()
		} else {
			err = a.opts.Startup.Disable()
		}
		if err != nil {
			a.log().Error("change start-at-login", "error", err)
			a.loginItem.SetChecked(was)
		}
	}()
}

func (a *app) togglePause() {
	next := !a.opts.Supervisor.Status().Paused
	a.opts.Supervisor.SetPaused(next)
	a.refresh()
}

// allowCurrentNetwork appends the live subnet to the allow-list by name, so
// the user never has to type a CIDR by hand for the common case of "trust
// where I am right now".
func (a *app) allowCurrentNetwork() {
	status := a.opts.Supervisor.Status()
	if !status.Network.Prefix.IsValid() {
		return
	}
	a.applyChange(func(s *config.Settings) {
		s.AllowedNetworks = append(s.AllowedNetworks, config.NetworkRule{
			Name:  fmt.Sprintf("Current (%s)", status.Network.Prefix),
			CIDRs: []string{status.Network.Prefix.String()},
		})
	}, nil, false)
}

func (a *app) openBrokerDialog() {
	if a.opts.Dialog == nil {
		openFile(a.opts.ConfigPath)
		return
	}
	go func() {
		cfg := a.opts.Supervisor.Config()
		settings, ok, err := a.opts.Dialog(cfg.Settings())
		if err != nil {
			a.log().Error("broker settings dialog", "error", err)
			return
		}
		if !ok {
			return
		}
		a.applyChange(func(s *config.Settings) {
			s.BrokerHost = settings.BrokerHost
			s.BrokerPort = settings.BrokerPort
			s.Username = settings.Username
			s.Password = settings.Password
		}, nil, false)
	}()
}

// openFile hands a path to the OS's default handler, so "Open config file"
// and "Open logs" behave the way a user already expects Explorer to.
func openFile(path string) {
	if path == "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}

// --- icon --------------------------------------------------------------

type iconColor int

const (
	colorGray iconColor = iota
	colorGreen
	colorYellow
	colorRed
)

// iconFor picks the tray icon colour. Red always means "on a call" — nothing
// else is allowed to produce it, since that is the one colour a user reacts
// to. Yellow flags a degraded-but-running state (paused, off an allowed
// network, or the broker unreachable) so a silently-not-working agent is
// still visible at a glance.
func iconFor(s engine.Status, brokerConnected bool) iconColor {
	if s.State == model.StateActive && !s.Paused {
		return colorRed
	}
	if s.State == model.StateUnknown {
		return colorGray
	}
	if s.Paused || !s.Allowed || !brokerConnected {
		return colorYellow
	}
	return colorGreen
}

// iconPNG renders a small solid dot, generated instead of embedded so the
// tray needs no asset files.
func iconPNG(c iconColor) []byte {
	rgba := map[iconColor]color.RGBA{
		colorGray:   {R: 130, G: 130, B: 130, A: 255},
		colorGreen:  {R: 30, G: 170, B: 70, A: 255},
		colorYellow: {R: 210, G: 170, B: 20, A: 255},
		colorRed:    {R: 210, G: 30, B: 30, A: 255},
	}[c]

	const size = 22
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy, r := size/2, size/2, size/2-1
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, rgba)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
