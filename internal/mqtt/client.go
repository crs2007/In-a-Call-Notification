// Package mqtt publishes call state to an MQTT broker and registers the
// matching Home Assistant entity.
//
// Most of the care here goes into one failure mode: a light left on after the
// call ended. The agent can crash, sleep, be killed or wander onto a network
// where it must stay silent, and in every one of those cases the state has to
// resolve itself without the agent's help. Four independent guards cover that,
// and none of them should be removed to simplify the code:
//
//   - a Will message, so an abrupt death publishes "offline"
//   - a clean shutdown that publishes "offline" before disconnecting
//   - a heartbeat that keeps re-asserting the current state
//   - expire_after in the Home Assistant discovery payload, so a silent agent
//     makes the entity unavailable rather than leaving it stuck on
package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"

	"github.com/crs2007/callmqtt/internal/config"
)

// Availability payloads. These are the strings Home Assistant is told to
// expect in the discovery config, so the two must stay in step.
const (
	Online  = "online"
	Offline = "offline"
)

// publishTimeout bounds how long a single publish may block waiting for the
// broker's acknowledgement, regardless of whatever ctx the caller happened to
// pass in. The engine's poll loop calls PublishState once per tick; without
// this bound a half-open socket could stall that call (and everything behind
// it) indefinitely instead of failing after one tick-plus-5s. It is layered
// on top of, not instead of, whatever deadline the caller's own ctx already
// carries: context.WithTimeout always honours the earlier of the two.
const publishTimeout = 5 * time.Second

// Payload is the body of a state message.
//
// It carries the call state and nothing that could identify a meeting. No
// window title, no meeting name, no participant ever belongs in this struct:
// it is the one thing in the project that leaves the machine.
type Payload struct {
	// Device names the machine this came from. Home Assistant already knows
	// it from the topic, but including it makes a wildcard subscription
	// self-describing, which matters as soon as there is more than one
	// machine in a room.
	Device     string    `json:"device"`
	State      string    `json:"state"`
	App        string    `json:"app,omitempty"`
	Apps       []string  `json:"apps,omitempty"`
	Confidence float64   `json:"confidence"`
	Network    string    `json:"network,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Options configures a Client.
type Options struct {
	Config  *config.Config
	Logger  *slog.Logger
	Version string // reported to Home Assistant as the device's software version
}

// Client is a connection to the broker that re-registers itself on every
// reconnect.
type Client struct {
	cm        *autopaho.ConnectionManager
	cfg       *config.Config
	log       *slog.Logger
	version   string
	connected atomic.Bool

	// stateSource is set once, at wiring time, by SetStateSource. It is a
	// *func rather than a plain field because onConnectionUp runs on
	// autopaho's own goroutine and may fire (on a reconnect) concurrently
	// with anything else touching the Client.
	stateSource atomic.Pointer[func() (Payload, bool)]
}

// New dials the broker and starts autopaho's reconnect loop. It returns as
// soon as the attempt is under way; use AwaitConnection to wait for success.
//
// ctx is the connection's lifetime, not the caller's: autopaho tears the
// connection down and stops reconnecting when ctx is cancelled, so it must
// outlive whatever operation happened to trigger this call (a request
// timeout, a settings-dialog deadline) and live as long as the client
// itself should stay connected.
func New(ctx context.Context, opts Options) (*Client, error) {
	cfg := opts.Config

	scheme := "mqtt"
	if cfg.MQTT.TLS.Enabled {
		scheme = "tls"
	}
	serverURL, err := url.Parse(fmt.Sprintf("%s://%s:%d", scheme, cfg.MQTT.Host, cfg.MQTT.Port))
	if err != nil {
		return nil, fmt.Errorf("build broker url: %w", err)
	}

	c := &Client{cfg: cfg, log: opts.Logger, version: opts.Version}

	clientCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{serverURL},
		KeepAlive:                     20,
		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         60,
		ConnectUsername:               cfg.MQTT.Username,
		ConnectPassword:               []byte(cfg.MQTT.Password),

		// The Will is the only guard that survives a power cut or a kill.
		WillMessage: &paho.WillMessage{
			Topic:   cfg.Topics.Availability,
			Payload: []byte(Offline),
			QoS:     cfg.MQTT.QoS,
			Retain:  true,
		},

		OnConnectionUp:   c.onConnectionUp,
		OnConnectionDown: c.onConnectionDown,
		OnConnectError: func(err error) {
			// Reconnecting is normal operation, not a fault: the broker may
			// simply not be up yet.
			c.log.Warn("mqtt connection attempt failed", "error", err)
		},

		ClientConfig: paho.ClientConfig{
			ClientID: cfg.MQTT.ClientID,
			// PacketTimeout bounds how long paho itself waits for a QoS
			// 1/2 PUBACK before giving up, independent of the ctx-based
			// publishTimeout above. The two are complementary rather than
			// redundant: publishTimeout bounds the call site (every
			// publish, regardless of which paho internals are involved),
			// while PacketTimeout is protocol-level and catches a stuck
			// PUBACK even on code paths that don't route through
			// Client.publish. It defaults to 10s if left unset, which is
			// longer than the 5s publishTimeout already guarantees, so an
			// unset value here would never actually fire first; matching
			// it to publishTimeout keeps the two guards in step instead of
			// carrying a second, looser number that could drift.
			PacketTimeout: publishTimeout,
			OnClientError: func(err error) {
				c.log.Warn("mqtt client error", "error", err)
			},
		},
	}

	tlsCfg, err := buildTLSConfig(cfg.MQTT.TLS)
	if err != nil {
		return nil, fmt.Errorf("configure tls: %w", err)
	}
	clientCfg.TlsCfg = tlsCfg

	cm, err := autopaho.NewConnection(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to broker %s: %w", serverURL.Host, err)
	}
	c.cm = cm

	return c, nil
}

// buildTLSConfig turns cfg.MQTT.TLS into a *tls.Config, or returns nil (no
// error) when TLS is not enabled. It is its own function, separate from New,
// so the CA-pool and client-keypair loading can be unit tested without
// dialing a broker.
//
// t.CAFile/CertFile/KeyFile are expected to already be paths this process
// can open outright: internal/config.Load resolves a relative one against
// the config file's directory before New ever sees it (the same convention
// rules_file uses), so this function does no path resolution of its own —
// only Parse-without-Load (tests, mainly) would hand it something still
// relative to the working directory.
//
// A bad or unreadable PEM file fails New outright rather than falling back
// to an insecure connection: this is a startup-time config problem, the same
// as an invalid broker URL, not something to log and limp past.
func buildTLSConfig(t config.TLS) (*tls.Config, error) {
	if !t.Enabled {
		return nil, nil
	}

	cfg := &tls.Config{
		InsecureSkipVerify: t.InsecureSkipVerify, //nolint:gosec // opt-in, documented
		MinVersion:         tls.VersionTLS12,
	}

	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read mqtt.tls.ca_file %s: %w", t.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("mqtt.tls.ca_file %s contains no usable certificates", t.CAFile)
		}
		cfg.RootCAs = pool
	}

	// config.Validate already rejects one of these being set without the
	// other, so by the time New runs this is "both set" or "neither" — but
	// checking both here rather than trusting that keeps this function
	// correct even if called directly (as the tests do) against a TLS value
	// that skipped Validate.
	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load mqtt.tls client keypair (cert_file %s, key_file %s): %w", t.CertFile, t.KeyFile, err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}

// connectionPublisher is the subset of *autopaho.ConnectionManager that
// publish (and, through it, everything published on reconnect) needs. It
// exists so republish can be unit tested against a fake broker connection
// instead of a live one — there is no fake broker in this package, and
// *autopaho.ConnectionManager can't be constructed without dialing.
type connectionPublisher interface {
	Publish(ctx context.Context, p *paho.Publish) (*paho.PublishResponse, error)
}

// SetStateSource registers the function republish calls, after "online" and
// the discovery config, to find out what state to republish on a (re)connect.
// It is meant to be called once, at wiring time, after both the engine and
// this Client exist — typically the engine's own Status() turned into a
// Payload — but onConnectionUp can run concurrently with that call on a later
// reconnect, hence the atomic store rather than a plain field.
//
// f is a pull, not a pushed value: the engine's current state can change
// between connects, and asking fresh here (rather than being handed a Payload
// up front) avoids ever republishing something that was already stale by the
// time a connect happened. ok == false means no state is known yet — e.g. at
// startup, before the engine's first evaluate() — in which case republish
// skips the extra publish rather than sending a meaningless zero-value
// Payload.
//
// This is also what makes mqtt.retain: false viable on the state topic: with
// retain on, a broker restart still has the retained message to fall back on
// even without this; with it off, a bare reconnect that only re-asserted
// "online" and the discovery config would leave the state topic silent until
// the next heartbeat. Republishing current state on every reconnect closes
// that gap regardless of the retain setting.
func (c *Client) SetStateSource(f func() (Payload, bool)) {
	c.stateSource.Store(&f)
}

// onConnectionUp re-establishes everything the broker forgets across a
// reconnect: that the agent is online, that the entity exists, and — via
// whatever SetStateSource wired up — what the current state actually is.
// Without this, a broker restart mid-call would leave Home Assistant with no
// entity and a stale, or (with retain disabled) entirely missing, state until
// the next heartbeat.
//
// autopaho requires this callback not to block, so the work runs detached, in
// republish.
func (c *Client) onConnectionUp(cm *autopaho.ConnectionManager, _ *paho.Connack) {
	c.log.Info("mqtt connected", "broker", c.cfg.MQTT.Host, "client_id", c.cfg.MQTT.ClientID)
	c.connected.Store(true)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c.republish(ctx, cm)
	}()
}

// republish is onConnectionUp's detached body: publish retained "online",
// the discovery config, and — if a state source has been wired and has
// something to report — the current state, in that order. Each step is
// attempted and logged independently: a failure publishing availability
// (say) must not skip discovery or state, since each of those still helps
// Home Assistant catch up as much as it can from a broker that just forgot
// everything.
func (c *Client) republish(ctx context.Context, cm connectionPublisher) {
	if err := c.publish(ctx, cm, c.cfg.Topics.Availability, []byte(Online), true); err != nil {
		c.log.Error("publish availability", "error", err)
	}
	if err := c.publishDiscovery(ctx, cm); err != nil {
		c.log.Error("publish home assistant discovery", "error", err)
	}

	src := c.stateSource.Load()
	if src == nil {
		return
	}
	payload, ok := (*src)()
	if !ok {
		// Nothing evaluated yet — e.g. connecting before the engine's first
		// poll. There is no stale value to worry about here: startup's own
		// "unknown" announcement (see engine.go) will follow shortly from the
		// normal publish path.
		return
	}
	if err := c.publishPayload(ctx, cm, payload, c.cfg.MQTT.Retain); err != nil {
		c.log.Error("publish state on reconnect", "error", err)
		return
	}
	c.log.Info("republished state on reconnect", "state", payload.State)
}

// onConnectionDown reports whether autopaho should keep retrying. It always
// should: the broker being unreachable is a transient condition, and giving up
// would strand the entity.
func (c *Client) onConnectionDown() bool {
	c.log.Warn("mqtt connection lost, reconnecting")
	c.connected.Store(false)
	return true
}

// Connected reports whether the broker connection is currently up. It is the
// tray's "Broker: connected" line — best-effort, and never a substitute for
// the Will/expire_after guards, which don't depend on this process noticing
// its own disconnect.
func (c *Client) Connected() bool { return c.connected.Load() }

// AwaitConnection blocks until the broker is connected or ctx expires.
func (c *Client) AwaitConnection(ctx context.Context) error {
	if err := c.cm.AwaitConnection(ctx); err != nil {
		return fmt.Errorf("await broker connection: %w", err)
	}
	return nil
}

// PublishState publishes the current call state.
func (c *Client) PublishState(ctx context.Context, p Payload) error {
	if err := c.publishPayload(ctx, c.cm, p, c.cfg.MQTT.Retain); err != nil {
		return err
	}
	c.log.Debug("published state", "topic", c.cfg.Topics.State, "state", p.State, "app", p.App)
	return nil
}

// publishPayload marshals p and publishes it to the state topic. It is the
// body PublishState and republish's reconnect republish both use, so a state
// message is always built from a Payload the same way regardless of which
// path sent it.
func (c *Client) publishPayload(ctx context.Context, cm connectionPublisher, p Payload, retain bool) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal state payload: %w", err)
	}
	return c.publish(ctx, cm, c.cfg.Topics.State, body, retain)
}

// Close publishes offline and disconnects cleanly. It is called on shutdown so
// that a deliberate quit releases the light immediately rather than waiting
// for the heartbeat to lapse.
func (c *Client) Close(ctx context.Context) error {
	if err := c.publish(ctx, c.cm, c.cfg.Topics.Availability, []byte(Offline), true); err != nil {
		// Worth reporting, but never worth blocking shutdown: the Will message
		// covers this case anyway.
		c.log.Warn("publish offline on shutdown", "error", err)
	}
	if err := c.cm.Disconnect(ctx); err != nil {
		return fmt.Errorf("disconnect from broker: %w", err)
	}
	return nil
}

func (c *Client) publish(ctx context.Context, cm connectionPublisher, topic string, body []byte, retain bool) error {
	ctx, cancel := boundedPublishContext(ctx)
	defer cancel()

	_, err := cm.Publish(ctx, &paho.Publish{
		Topic:   topic,
		Payload: body,
		QoS:     c.cfg.MQTT.QoS,
		Retain:  retain,
	})
	if err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

// boundedPublishContext derives the ctx actually used for a single publish
// call. It exists as its own function so the bound can be asserted on in a
// test without needing a live broker connection: context.WithTimeout already
// takes whichever of the caller's own deadline and publishTimeout is sooner,
// so this never lengthens a caller-supplied deadline, only ever shortens an
// absent or overly generous one.
func boundedPublishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, publishTimeout)
}
