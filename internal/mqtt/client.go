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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
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
			OnClientError: func(err error) {
				c.log.Warn("mqtt client error", "error", err)
			},
		},
	}

	if cfg.MQTT.TLS.Enabled {
		clientCfg.TlsCfg = &tls.Config{
			InsecureSkipVerify: cfg.MQTT.TLS.InsecureSkipVerify, //nolint:gosec // opt-in, documented
			MinVersion:         tls.VersionTLS12,
		}
	}

	cm, err := autopaho.NewConnection(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to broker %s: %w", serverURL.Host, err)
	}
	c.cm = cm

	return c, nil
}

// onConnectionUp re-establishes everything the broker forgets across a
// reconnect: that the agent is online, that the entity exists, and what the
// current state is. Without this, a broker restart mid-call would leave Home
// Assistant with no entity and a stale state.
//
// autopaho requires this callback not to block, so the work runs detached.
func (c *Client) onConnectionUp(cm *autopaho.ConnectionManager, _ *paho.Connack) {
	c.log.Info("mqtt connected", "broker", c.cfg.MQTT.Host, "client_id", c.cfg.MQTT.ClientID)
	c.connected.Store(true)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := c.publish(ctx, cm, c.cfg.Topics.Availability, []byte(Online), true); err != nil {
			c.log.Error("publish availability", "error", err)
		}
		if err := c.publishDiscovery(ctx, cm); err != nil {
			c.log.Error("publish home assistant discovery", "error", err)
		}
	}()
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
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal state payload: %w", err)
	}
	if err := c.publish(ctx, c.cm, c.cfg.Topics.State, body, c.cfg.MQTT.Retain); err != nil {
		return err
	}
	c.log.Debug("published state", "topic", c.cfg.Topics.State, "state", p.State, "app", p.App)
	return nil
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

func (c *Client) publish(ctx context.Context, cm *autopaho.ConnectionManager, topic string, body []byte, retain bool) error {
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
