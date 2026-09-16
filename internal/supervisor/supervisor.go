// Package supervisor keeps the engine and MQTT client running across a
// config change.
//
// A settings menu whose effect you cannot see invites people to doubt it
// worked, so Reload rebuilds the detection engine and the broker connection
// from a new config without dropping the process. Everything that can fail
// on its own — a detector that won't start, a malformed network rule — is
// validated before the live generation is touched, so a bad reload leaves
// it running rather than tearing it down for nothing. The broker connection
// itself is swapped the other way around: the old one is stopped before the
// new one dials, so the two never briefly share one ClientID (see Reload).
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

	// ctx is the root of every generation's publisher connection. It is
	// independent of any ctx a caller passes to Start/Reload, so a caller's
	// deadline (e.g. the tray's 15s "apply this setting" timeout) bounds only
	// the build step and never reaches down into the broker connection.
	ctx    context.Context
	cancel context.CancelFunc
}

// generation is one running (engine, publisher) pair and the means to stop it.
type generation struct {
	eng       *engine.Engine
	pub       Publisher
	cancel    context.CancelFunc // stops eng.Run's poll loop
	pubCancel context.CancelFunc // releases the publisher's connection ctx
	done      chan struct{}
}

// New builds a Supervisor. Call Start to bring up the first generation.
func New(opts Options) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		log:     opts.Logger,
		version: opts.Version,
		checker: opts.Checker,
		newDets: opts.NewDetectors,
		newPub:  opts.NewPublisher,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start builds and runs the first generation from cfg.
func (s *Supervisor) Start(ctx context.Context, cfg *config.Config) error {
	detectors, err := s.validate(cfg)
	if err != nil {
		return err
	}
	gen, err := s.build(ctx, cfg, detectors)
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

// Reload rebuilds the engine and publisher from cfg and swaps them in.
//
// Everything that can fail independently of the broker — detectors, network
// rules — is validated before the current generation is touched, so a bad
// reload (a typo'd detector name, a malformed CIDR) leaves it running. The
// broker dial itself is not part of that guarantee: autopaho's connection
// attempt doesn't fail on reachability (it just retries), so there is
// nothing to protect by building the new publisher before stopping the old
// one — and doing so would leave two clients briefly sharing one ClientID,
// causing a broker-side session takeover that fires the old Will and can
// leave the retained availability at "offline" after "online" is published.
// Stopping the old generation first avoids that race entirely.
func (s *Supervisor) Reload(ctx context.Context, cfg *config.Config) error {
	detectors, err := s.validate(cfg)
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}

	s.mu.Lock()
	old := s.gen
	s.mu.Unlock()

	if old != nil {
		s.stop(ctx, old)
	}

	next, err := s.build(ctx, cfg, detectors)
	if err != nil {
		// The broker dial itself doesn't fail on reachability, so getting
		// here means something rarer (a malformed broker URL, say). Either
		// way old is already stopped: there is no live generation left.
		s.mu.Lock()
		s.gen = nil
		s.mu.Unlock()
		return fmt.Errorf("reload: %w", err)
	}

	if old != nil {
		// A settings change (say, toggling HA discovery) should not silently
		// resume detection out from under a pause the user set deliberately.
		next.eng.SetPaused(old.eng.Paused())
	}

	s.mu.Lock()
	s.cfg = cfg
	s.gen = next
	s.mu.Unlock()
	s.run(next)
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
	defer s.cancel()

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

// validate builds the parts of a generation that can fail independently of
// the broker — detectors and the network allow-list — without touching the
// currently running generation or dialing anything. Callers that need the
// matcher itself (engine.New) build their own; this is purely a fail-fast
// check so Reload knows before it stops the old generation.
func (s *Supervisor) validate(cfg *config.Config) ([]model.Detector, error) {
	detectors, err := s.newDets(cfg)
	if err != nil {
		return nil, fmt.Errorf("build detectors: %w", err)
	}
	if _, err := network.NewMatcher(cfg.AllowedNetworks); err != nil {
		return nil, fmt.Errorf("build network matcher: %w", err)
	}
	return detectors, nil
}

// build constructs a generation from cfg and already-validated detectors,
// without starting it or touching any existing generation.
//
// The publisher's connection ctx is a child of the Supervisor's own root
// ctx, not of the ctx passed in here: ctx bounds only this build call (and
// any future AwaitConnection added here), never the connection itself. That
// keeps a caller's deadline — e.g. the tray's timeout on applying one
// setting — from tearing down the broker connection when it expires.
func (s *Supervisor) build(ctx context.Context, cfg *config.Config, detectors []model.Detector) (*generation, error) {
	pubCtx, pubCancel := context.WithCancel(s.ctx)

	pub, err := s.newPub(pubCtx, cfg, s.log, s.version)
	if err != nil {
		pubCancel()
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
		pubCancel()
		return nil, fmt.Errorf("build engine: %w", err)
	}

	return &generation{eng: eng, pub: pub, pubCancel: pubCancel, done: make(chan struct{})}, nil
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
// goroutine-leak guarantee — closes its publisher, and finally releases the
// publisher's connection ctx.
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
	gen.pubCancel()
}
