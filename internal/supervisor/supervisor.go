// Package supervisor keeps the engine and MQTT client running across a
// config change.
//
// A settings menu whose effect you cannot see invites people to doubt it
// worked, so Reload rebuilds the detection engine and the broker connection
// from a new config without dropping the process. It always builds the
// replacement before touching what is currently running: if the new config
// turns out to be bad (unreachable broker, a detector that fails to start),
// the live generation is left untouched rather than torn down for nothing.
package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/engine"
	"github.com/crs2007/callmqtt/internal/model"
	"github.com/crs2007/callmqtt/internal/network"
)

// Publisher is what a generation needs from its broker connection: the
// engine's publish call, plus a clean way to let go of it on reload.
type Publisher interface {
	engine.Publisher
	Close(ctx context.Context) error
	Connected() bool
}

// Options collects the Supervisor's dependencies. NewDetectors and
// NewPublisher are factories rather than concrete values so tests can supply
// fakes and never touch a real broker or the Windows platform layer.
type Options struct {
	Logger       *slog.Logger
	Version      string
	Checker      network.Checker
	NewDetectors func(*config.Config) ([]model.Detector, error)
	NewPublisher func(ctx context.Context, cfg *config.Config, log *slog.Logger, version string) (Publisher, error)
}

// Supervisor owns the currently running engine generation.
type Supervisor struct {
	log     *slog.Logger
	version string
	checker network.Checker
	newDets func(*config.Config) ([]model.Detector, error)
	newPub  func(context.Context, *config.Config, *slog.Logger, string) (Publisher, error)

	mu  sync.Mutex
	cfg *config.Config
	gen *generation
}

// generation is one running (engine, publisher) pair and the means to stop it.
type generation struct {
	eng    *engine.Engine
	pub    Publisher
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds a Supervisor. Call Start to bring up the first generation.
func New(opts Options) *Supervisor {
	return &Supervisor{
		log:     opts.Logger,
		version: opts.Version,
		checker: opts.Checker,
		newDets: opts.NewDetectors,
		newPub:  opts.NewPublisher,
	}
}

// Start builds and runs the first generation from cfg.
func (s *Supervisor) Start(ctx context.Context, cfg *config.Config) error {
	gen, err := s.build(ctx, cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.gen = gen
	s.run(gen)
	return nil
}

// Reload rebuilds the engine and publisher from cfg and swaps them in. The
// previous generation is only stopped after the new one has been built
// successfully, so a bad reload leaves the current generation running.
func (s *Supervisor) Reload(ctx context.Context, cfg *config.Config) error {
	next, err := s.build(ctx, cfg)
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}

	s.mu.Lock()
	old := s.gen
	if old != nil {
		// A settings change (say, toggling HA discovery) should not silently
		// resume detection out from under a pause the user set deliberately.
		next.eng.SetPaused(old.eng.Paused())
	}
	s.cfg = cfg
	s.gen = next
	s.run(next)
	s.mu.Unlock()

	if old != nil {
		s.stop(ctx, old)
	}
	return nil
}

// Status returns the live generation's most recent snapshot.
func (s *Supervisor) Status() engine.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == nil {
		return engine.Status{State: model.StateUnknown}
	}
	return s.gen.eng.Status()
}

// Config returns the config the running generation was built from.
func (s *Supervisor) Config() *config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// SetPaused turns detection on or off on the running generation without a
// reload. It is not persisted to config: pausing is a transient tray action,
// not a considered setting change.
func (s *Supervisor) SetPaused(paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != nil {
		s.gen.eng.SetPaused(paused)
	}
}

// BrokerConnected reports whether the running generation's publisher
// currently has a live broker connection.
func (s *Supervisor) BrokerConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == nil {
		return false
	}
	return s.gen.pub.Connected()
}

// Close stops the running generation. Its publisher's Close is responsible
// for releasing the light (e.g. publishing "offline") before returning.
func (s *Supervisor) Close(ctx context.Context) error {
	s.mu.Lock()
	gen := s.gen
	s.gen = nil
	s.mu.Unlock()

	if gen == nil {
		return nil
	}
	s.stop(ctx, gen)
	return nil
}

// build constructs a generation from cfg without starting it or touching any
// existing generation.
func (s *Supervisor) build(ctx context.Context, cfg *config.Config) (*generation, error) {
	detectors, err := s.newDets(cfg)
	if err != nil {
		return nil, fmt.Errorf("build detectors: %w", err)
	}

	pub, err := s.newPub(ctx, cfg, s.log, s.version)
	if err != nil {
		return nil, fmt.Errorf("build publisher: %w", err)
	}

	eng, err := engine.New(engine.Options{
		Config:    cfg,
		Logger:    s.log,
		Detectors: detectors,
		Checker:   s.checker,
		Publisher: pub,
	})
	if err != nil {
		_ = pub.Close(ctx)
		return nil, fmt.Errorf("build engine: %w", err)
	}

	return &generation{eng: eng, pub: pub, done: make(chan struct{})}, nil
}

// run starts a generation's Run loop in the background, in its own cancelable
// context so stop can end it independently of any caller's ctx.
func (s *Supervisor) run(gen *generation) {
	runCtx, cancel := context.WithCancel(context.Background())
	gen.cancel = cancel
	go func() {
		defer close(gen.done)
		_ = gen.eng.Run(runCtx)
	}()
}

// stop cancels a generation's Run loop, waits for it to actually exit — the
// goroutine-leak guarantee — and then closes its publisher.
func (s *Supervisor) stop(ctx context.Context, gen *generation) {
	gen.cancel()

	select {
	case <-gen.done:
	case <-time.After(5 * time.Second):
		s.log.Warn("supervisor: engine did not stop in time, closing publisher anyway")
	}

	if err := gen.pub.Close(ctx); err != nil {
		s.log.Warn("supervisor: close publisher", "error", err)
	}
}
