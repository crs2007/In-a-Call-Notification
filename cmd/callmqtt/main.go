// Command callmqtt detects whether you are in a Zoom, Teams or Slack call and
// publishes that state to a local MQTT broker, so Home Assistant can drive a
// "do not disturb" light for exactly as long as the call lasts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/engine"
	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/mqtt"
	"github.com/crs2007/callmqtt/internal/network"
	"github.com/crs2007/callmqtt/internal/simulate"
	"github.com/crs2007/callmqtt/internal/supervisor"
)

// version is overridden at build time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

type flags struct {
	configPath     string
	debug          bool
	validateConfig bool
	printConfig    bool
	once           bool
	simulate       bool
	showVersion    bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "callmqtt:", err)
		os.Exit(1)
	}
}

func run() error {
	var f flags
	flag.StringVar(&f.configPath, "config", defaultConfigPath(), "path to config.yaml")
	flag.BoolVar(&f.debug, "debug", false, "log every poll to stderr")
	flag.BoolVar(&f.validateConfig, "validate-config", false, "check the config and exit")
	flag.BoolVar(&f.printConfig, "print-config", false, "print the effective config, secrets redacted, and exit")
	flag.BoolVar(&f.once, "once", false, "run one detection cycle, print the result, and exit")
	flag.BoolVar(&f.simulate, "simulate", false, "fake a call every 30s, to test a Home Assistant automation without joining one")
	flag.BoolVar(&f.showVersion, "version", false, "print the version and exit")
	flag.Usage = usage
	flag.Parse()

	if f.showVersion {
		fmt.Println("callmqtt", version)
		return nil
	}

	if flag.Arg(0) == "init" {
		return initConfig(f.configPath)
	}

	cfg, err := config.Load(f.configPath)
	if err != nil {
		if os.IsNotExist(errors.Unwrap(err)) {
			return fmt.Errorf("no config at %s\n\nRun `callmqtt init` to create a starter config, then edit it", f.configPath)
		}
		return err
	}

	if f.validateConfig {
		fmt.Printf("%s is valid\n", f.configPath)
		return nil
	}
	if f.printConfig {
		return printConfig(cfg)
	}

	log, closeLog, err := newLogger(cfg, f.debug)
	if err != nil {
		return err
	}
	defer closeLog()

	if cfg.PasswordIsLiteral() {
		log.Warn("mqtt password is written directly in the config file; prefer ${ENV_VAR}")
	}

	// Signals are handled before anything connects, so an interrupt during a
	// slow broker connection still shuts down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if f.once {
		detectors, err := buildDetectors(cfg, f)
		if err != nil {
			return err
		}
		return runOnce(ctx, cfg, log, detectors)
	}

	return runAgent(ctx, cfg, log, f)
}

// runAgent runs the supervisor — engine plus MQTT client, rebuildable live —
// and, in a tray build, the tray UI on top of it. detectors and the publisher
// are rebuilt by the supervisor on every config reload, so the closures here
// are the only place that needs to know how to build them.
func runAgent(ctx context.Context, cfg *config.Config, log *slog.Logger, f flags) error {
	sup := supervisor.New(supervisor.Options{
		Logger:  log,
		Version: version,
		Checker: network.LocalChecker{},
		NewDetectors: func(cfg *config.Config) ([]model.Detector, error) {
			return buildDetectors(cfg, f)
		},
		NewPublisher: func(ctx context.Context, cfg *config.Config, log *slog.Logger, version string) (supervisor.Publisher, error) {
			return mqtt.New(ctx, mqtt.Options{Config: cfg, Logger: log, Version: version})
		},
	})

	if err := sup.Start(ctx, cfg); err != nil {
		return err
	}

	log.Info("callmqtt started",
		"version", version, "device_id", cfg.App.DeviceID,
		"broker", fmt.Sprintf("%s:%d", cfg.MQTT.Host, cfg.MQTT.Port))

	uiErr := runUI(ctx, sup, log, f.configPath, cfg.Logging.File)

	// Shutdown gets its own context: ctx may already be cancelled (SIGINT, or
	// the tray's Quit), and publishing "offline" on the way out is what
	// releases the light immediately instead of waiting for the heartbeat to
	// lapse.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sup.Close(shutdownCtx); err != nil {
		log.Warn("shutdown", "error", err)
	}
	log.Info("callmqtt stopped")

	if errors.Is(uiErr, context.Canceled) {
		return nil
	}
	return uiErr
}

// runOnce evaluates once and prints what it found, without touching the
// broker. It is the quickest way to check wiring on a new machine.
func runOnce(ctx context.Context, cfg *config.Config, log *slog.Logger, detectors []model.Detector) error {
	eng, err := engine.New(engine.Options{
		Config:    cfg,
		Logger:    log,
		Detectors: detectors,
		Checker:   network.LocalChecker{},
		Publisher: discardPublisher{},
	})
	if err != nil {
		return err
	}

	eng.Evaluate(ctx, time.Now())
	s := eng.Status()

	fmt.Printf("state:      %s\n", s.State)
	if s.App != "" {
		fmt.Printf("app:        %s\n", s.App)
	}
	fmt.Printf("confidence: %.2f\n", s.Confidence)
	fmt.Printf("interface:  %s\n", orDash(s.Network.Interface))
	fmt.Printf("address:    %s\n", orDash(addrString(s.Network)))
	fmt.Printf("network:    %s\n", orDash(s.NetworkRule))
	fmt.Printf("publishing: %v\n", s.Allowed)
	if len(s.Reasons) > 0 {
		fmt.Printf("reasons:    %v\n", s.Reasons)
	}
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `callmqtt %s - publish desktop call presence to MQTT

Usage:
  callmqtt [flags]        run the agent
  callmqtt init           write a starter config and print where it went

Flags:
`, version)
	flag.PrintDefaults()
}

// initConfig writes the annotated example config to path, and tells the user
// the two things they now have to decide: where their broker is, and which
// networks they are willing to publish from.
func initConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; edit it, or delete it first", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, config.Example, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf(`Wrote %s

This machine will appear as device id %q, so its topics are:
  desktop-presence/%s/call
  desktop-presence/%s/availability

Before starting, edit that file and set:
  mqtt.host          your broker's address
  mqtt.username      if your broker requires one
  allowed_networks   the subnets you are willing to publish from

The password is read from the CALLMQTT_MQTT_PASSWORD environment variable.
Then check your work with:  callmqtt --validate-config
`, path, config.AutoDeviceID(), config.AutoDeviceID(), config.AutoDeviceID())
	return nil
}

// discardPublisher lets --once exercise the whole engine without a broker.
type discardPublisher struct{}

func (discardPublisher) PublishState(context.Context, mqtt.Payload) error { return nil }

func buildDetectors(_ *config.Config, f flags) ([]model.Detector, error) {
	if f.simulate {
		return []model.Detector{simulate.NewDetector()}, nil
	}
	// Real detectors arrive with the Windows platform adapters. Until then,
	// refuse rather than silently reporting that nobody is ever in a call:
	// an agent that is confidently wrong is worse than one that says so.
	return nil, errors.New("no detectors are available yet in this build; run with -simulate")
}

// newLogger writes human-readable output to stderr and, when configured, JSON
// to a log file. Info level records state transitions only, so the log stays
// readable across a full working day.
func newLogger(cfg *config.Config, debug bool) (*slog.Logger, func(), error) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	} else {
		switch cfg.Logging.Level {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	handlers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
	}
	closeFn := func() {}

	if cfg.Logging.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Logging.File), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}
		file, err := os.OpenFile(cfg.Logging.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		handlers = append(handlers, slog.NewJSONHandler(file, &slog.HandlerOptions{Level: level}))
		closeFn = func() { _ = file.Close() }
	}

	return slog.New(multiHandler(handlers)), closeFn, nil
}

func printConfig(cfg *config.Config) error {
	redacted := cfg.Redacted()
	body, err := json.MarshalIndent(struct {
		DeviceID string               `json:"device_id"`
		Topics   config.Topics        `json:"topics"`
		MQTT     config.MQTT          `json:"mqtt"`
		Poll     config.Poll          `json:"poll"`
		Detect   config.Detection     `json:"detection"`
		Networks []config.NetworkRule `json:"allowed_networks"`
		Logging  config.Logging       `json:"logging"`
	}{
		DeviceID: redacted.App.DeviceID,
		Topics:   redacted.Topics,
		MQTT:     redacted.MQTT,
		Poll:     redacted.Poll,
		Detect:   redacted.Detection,
		Networks: redacted.AllowedNetworks,
		Logging:  redacted.Logging,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	fmt.Println(string(body))
	return nil
}

// defaultConfigPath puts the config beside the log, in the per-user config
// directory, so nothing needs administrator rights.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(dir, "callmqtt", "config.yaml")
}

func addrString(info network.Info) string {
	if !info.LocalIP.IsValid() {
		return ""
	}
	if info.Prefix.IsValid() {
		return fmt.Sprintf("%s (%s)", info.LocalIP, info.Prefix)
	}
	return info.LocalIP.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// multiHandler fans one record out to several handlers, so the same event can
// be human-readable on stderr and structured in the log file.
type multiHandler []slog.Handler

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithGroup(name)
	}
	return out
}
