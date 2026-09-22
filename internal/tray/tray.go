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
)

// Startup manages whether the agent launches at login. Implementations live
// in platform/. Left nil, the tray simply omits the checkbox rather than
// showing a control that does nothing.
type Startup interface {
	IsEnabled() (bool, error)
	Enable(configPath string) error
	Disable() error
}

// Dialog collects broker settings from the user and reports whether they
// confirmed. Left nil, "Broker settings…" falls back to opening the config
// file, since there is otherwise no way to change those four fields at all.
type Dialog func(current config.Settings) (config.Settings, bool, error)

// supervisorAPI is exactly what app needs from a *supervisor.Supervisor.
// Depending on the interface rather than the concrete type lets tests hand
// app a fake instead of a live engine and broker connection.
type supervisorAPI interface {
	Config() *config.Config
	Status() engine.Status
	Reload(ctx context.Context, cfg *config.Config) error
	SetPaused(paused bool)
	Paused() bool
	BrokerConnected() bool
}

// Options configures the tray.
type Options struct {
	Supervisor   supervisorAPI
	Logger       *slog.Logger
	ConfigPath   string
	LogPath      string
	PollInterval time.Duration // default 2s
	Dialog       Dialog
	Startup      Startup
}

// Run builds the tray icon and menu and blocks pumping the OS message loop
// until Quit is chosen or ctx is cancelled (e.g. Ctrl-C from a console).
// Call it from main after everything else is wired up.
func Run(ctx context.Context, opts Options) error {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}

	a := &app{opts: opts, tray: systray.New(), refreshNow: make(chan struct{}, 1)}
	a.buildMenu()

	a.tray.SetIcon(iconPNG(trayIconState{color: colorGray})).
		SetTooltip("In a Call Notification | On call: starting | MQTT: disconnected").
		SetMenu(a.menu)
	a.tray.Show()

	stop := make(chan struct{})
	go a.refreshLoop(stop)
	defer close(stop)

	// Remove() posts the platform's quit signal, the same one the "Quit" menu
	// item sends, so a signal-driven shutdown shares the exact exit path a
	// deliberate quit takes — and, crucially, lets Run() return so main can
	// reach sup.Close() and publish "offline".
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			a.tray.Remove()
		case <-watchDone:
		}
	}()
	defer close(watchDone)

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

	// refreshNow lets other goroutines request a refresh without calling
	// a.refresh() themselves, so refreshLoop's goroutine stays the only one
	// that ever touches lastIcon. Buffered 1 with a non-blocking send: a
	// refresh already queued doesn't need a second one behind it.
	refreshNow chan struct{}

	lastIcon trayIconState
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

	m.Add("MQTT broker settings…", func() { a.openBrokerDialog() })
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
		case <-a.refreshNow:
			a.refresh()
		}
	}
}

func (a *app) refresh() {
	status := a.opts.Supervisor.Status()
	// SetPaused flips the engine's atomic flag without re-running evaluate,
	// so the snapshot's Paused can lag by up to one poll interval. Paused()
	// reads the live flag directly, keeping every label below correct right
	// after a toggle.
	status.Paused = a.opts.Supervisor.Paused()
	brokerConnected := a.opts.Supervisor.BrokerConnected()

	a.stateItem.SetLabel(stateLabel(status))
	a.networkItem.SetLabel(networkLabel(status))
	a.brokerItem.SetLabel(brokerLabel(brokerConnected))
	a.tray.SetTooltip(statusTooltip(status, brokerConnected))

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

	icon := iconFor(status, brokerConnected)
	if icon != a.lastIcon {
		a.tray.SetIcon(iconPNG(icon))
		a.lastIcon = icon
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

func statusTooltip(s engine.Status, brokerConnected bool) string {
	call := "not on a call"
	if s.Paused {
		call = "detection paused"
	} else if s.State == model.StateUnknown {
		call = "starting"
	} else if s.State == model.StateActive {
		app := s.App
		if app == "" {
			app = strings.Join(s.Apps, ", ")
		}
		call = "yes"
		if app != "" {
			call += " (" + title(app) + ")"
		}
	}

	mqttStatus := "disconnected"
	if brokerConnected {
		mqttStatus = "connected"
	}
	return fmt.Sprintf("In a Call Notification | On call: %s | MQTT: %s", call, mqttStatus)
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
		a.requestRefresh()
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
			err = a.opts.Startup.Enable(a.opts.ConfigPath)
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
	next := !a.opts.Supervisor.Paused()
	a.opts.Supervisor.SetPaused(next)
	a.requestRefresh()
}

// requestRefresh asks refreshLoop to run a.refresh() on its own goroutine, so
// callers on other goroutines (the systray callback goroutine, applyChange's
// spawned goroutines) never touch lastIcon themselves.
func (a *app) requestRefresh() {
	select {
	case a.refreshNow <- struct{}{}:
	default:
	}
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
			s.PasswordChanged = settings.PasswordChanged
		}, nil, false)
	}()
}

// openFile hands a path to the OS's default handler, so "Open config file"
// and "Open logs" behave the way a user already expects Explorer to.
//
// Windows gets its own openFileOS (openfile_windows.go), calling
// ShellExecute directly instead of shelling out to `cmd /c start`; every
// other OS still goes through exec.Command, since there is no equivalent
// direct syscall this package makes for them.
func openFile(path string) {
	if path == "" {
		return
	}
	if runtime.GOOS == "windows" {
		openFileOS(path)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
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

type trayIconState struct {
	color           iconColor
	brokerConnected bool
}

// iconFor combines call/detection state in the broadcast glyph with MQTT
// connectivity in a persistent badge, so neither signal can hide the other.
func iconFor(s engine.Status, brokerConnected bool) trayIconState {
	state := trayIconState{brokerConnected: brokerConnected}
	if s.State == model.StateActive && !s.Paused {
		state.color = colorRed
		return state
	}
	if s.State == model.StateUnknown {
		state.color = colorGray
		return state
	}
	if s.Paused || !s.Allowed {
		state.color = colorYellow
		return state
	}
	state.color = colorGreen
	return state
}

// iconPNG renders the call-state broadcast glyph plus a green/red MQTT badge.
func iconPNG(state trayIconState) []byte {
	rgba := map[iconColor]color.RGBA{
		colorGray:   {R: 140, G: 140, B: 140, A: 255},
		colorGreen:  {R: 34, G: 197, B: 94, A: 255},
		colorYellow: {R: 245, G: 158, B: 11, A: 255},
		colorRed:    {R: 239, G: 68, B: 68, A: 255},
	}[state.color]

	const size = 22
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	const (
		cx              = 4.5
		cy              = 17.5
		rDotSq          = 2.5 * 2.5
		r1InSq, r1OutSq = 5.4 * 5.4, 7.8 * 7.8
		r2InSq, r2OutSq = 9.8 * 9.8, 12.2 * 12.2
		r3InSq, r3OutSq = 14.2 * 14.2, 16.6 * 16.6
		samples         = 4
	)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			hits := 0
			for sy := 0; sy < samples; sy++ {
				py := float64(y) + (float64(sy)+0.5)/float64(samples)
				dy := cy - py
				for sx := 0; sx < samples; sx++ {
					px := float64(x) + (float64(sx)+0.5)/float64(samples)
					dx := px - cx

					d2 := dx*dx + dy*dy
					// Base transmitter node
					if d2 <= rDotSq {
						hits++
						continue
					}
					// MQTT broadcast waves radiating upward and rightward
					if dx >= -0.2 && dy >= -0.2 {
						if (d2 >= r1InSq && d2 <= r1OutSq) ||
							(d2 >= r2InSq && d2 <= r2OutSq) ||
							(d2 >= r3InSq && d2 <= r3OutSq) {
							hits++
						}
					}
				}
			}

			if hits > 0 {
				alpha := uint8((hits * 255) / (samples * samples))
				img.SetRGBA(x, y, color.RGBA{
					R: uint8((uint32(rgba.R) * uint32(alpha)) / 255),
					G: uint8((uint32(rgba.G) * uint32(alpha)) / 255),
					B: uint8((uint32(rgba.B) * uint32(alpha)) / 255),
					A: alpha,
				})
			}
		}
	}

	badge := color.RGBA{R: 239, G: 68, B: 68, A: 255}
	if state.brokerConnected {
		badge = color.RGBA{R: 34, G: 197, B: 94, A: 255}
	}
	for y := 14; y < size; y++ {
		for x := 14; x < size; x++ {
			dx := float64(x) + 0.5 - 17.5
			dy := float64(y) + 0.5 - 17.5
			d2 := dx*dx + dy*dy
			switch {
			case d2 <= 3.6*3.6:
				img.SetRGBA(x, y, color.RGBA{R: 32, G: 32, B: 32, A: 255})
			case d2 <= 4.2*4.2:
				img.SetRGBA(x, y, color.RGBA{A: 180})
			}
			if d2 <= 2.5*2.5 {
				img.SetRGBA(x, y, badge)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
