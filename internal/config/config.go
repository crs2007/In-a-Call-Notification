// Package config loads, defaults and validates the CallMQTT YAML configuration.
//
// Validation reports every problem at once rather than stopping at the first,
// because a user fixing their broker settings should not have to run the agent
// six times to discover six typos.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Example is the annotated starter configuration written by `callmqtt init`.
// It is embedded so the binary is self-sufficient: a user who downloaded only
// the exe can still produce a working config.
//
//go:embed example.yaml
var Example []byte

// Config is the whole of CallMQTT's configuration.
type Config struct {
	App             App                 `yaml:"app"`
	Poll            Poll                `yaml:"poll"`
	Detection       Detection           `yaml:"detection"`
	AllowedNetworks []NetworkRule       `yaml:"allowed_networks"`
	MQTT            MQTT                `yaml:"mqtt"`
	Topics          Topics              `yaml:"topics"`
	Detectors       map[string]Detector `yaml:"detectors"`
	RulesFile       string              `yaml:"rules_file"`
	Logging         Logging             `yaml:"logging"`

	// passwordFromEnv records whether mqtt.password referenced ${VAR} in the
	// file. It can only be determined before expansion, so Parse captures it.
	passwordFromEnv bool
}

// App identifies this machine.
type App struct {
	// DeviceID identifies this machine in MQTT topics. "auto" derives it from
	// the hostname.
	DeviceID string `yaml:"device_id"`
}

// Poll controls how often the agent looks at the world.
type Poll struct {
	DetectSeconds    int `yaml:"detect_seconds"`
	NetworkSeconds   int `yaml:"network_seconds"`
	HeartbeatSeconds int `yaml:"heartbeat_seconds"`
}

// Detect returns the detector poll interval.
func (p Poll) Detect() time.Duration { return time.Duration(p.DetectSeconds) * time.Second }

// Network returns the network check interval.
func (p Poll) Network() time.Duration { return time.Duration(p.NetworkSeconds) * time.Second }

// Heartbeat returns the interval at which current state is republished.
func (p Poll) Heartbeat() time.Duration { return time.Duration(p.HeartbeatSeconds) * time.Second }

// Detection holds the thresholds that turn a confidence score into a state.
//
// The debounces are deliberately asymmetric: entering a call should feel
// immediate, while leaving one is held briefly so that a brief reconnect or a
// camera toggle cannot flicker the light.
type Detection struct {
	ActiveThreshold      float64 `yaml:"active_threshold"`
	InactiveThreshold    float64 `yaml:"inactive_threshold"`
	EnterDebounceSeconds int     `yaml:"enter_debounce_seconds"`
	ExitDebounceSeconds  int     `yaml:"exit_debounce_seconds"`
}

// EnterDebounce returns how long an active reading must persist before it counts.
func (d Detection) EnterDebounce() time.Duration {
	return time.Duration(d.EnterDebounceSeconds) * time.Second
}

// ExitDebounce returns how long an inactive reading must persist before it counts.
func (d Detection) ExitDebounce() time.Duration {
	return time.Duration(d.ExitDebounceSeconds) * time.Second
}

// NetworkRule describes one network on which publishing is permitted.
// The fields are alternatives: matching any one of them matches the rule.
// omitempty keeps the file tidy when the tray rewrites the allow-list: a rule
// matched purely by subnet should not sprout three empty lists.
type NetworkRule struct {
	Name     string   `yaml:"name"`
	SSIDs    []string `yaml:"ssids,omitempty"`
	BSSIDs   []string `yaml:"bssids,omitempty"`
	CIDRs    []string `yaml:"cidrs,omitempty"`
	Gateways []string `yaml:"gateways,omitempty"`
}

// MQTT holds broker connection settings.
type MQTT struct {
	Host      string    `yaml:"host"`
	Port      int       `yaml:"port"`
	Username  string    `yaml:"username"`
	Password  string    `yaml:"password"`
	ClientID  string    `yaml:"client_id"`
	QoS       byte      `yaml:"qos"`
	Retain    bool      `yaml:"retain"`
	TLS       TLS       `yaml:"tls"`
	Discovery Discovery `yaml:"discovery"`
}

// TLS controls transport security to the broker.
type TLS struct {
	Enabled            bool `yaml:"enabled"`
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// Discovery controls Home Assistant MQTT Discovery, which creates the
// binary_sensor entity automatically so no hand-written HA YAML is needed.
type Discovery struct {
	Enabled bool   `yaml:"enabled"`
	Prefix  string `yaml:"prefix"`
}

// Topics names the two topics the agent publishes to.
type Topics struct {
	State        string `yaml:"state"`
	Availability string `yaml:"availability"`
}

// Detector switches one application's detection on or off.
type Detector struct {
	Enabled bool `yaml:"enabled"`
}

// Logging controls log verbosity and destination.
type Logging struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

// envPattern matches ${NAME} only. A bare $NAME is left alone so that Windows
// paths and detection regexes in the config cannot be mangled by accident.
var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${NAME} with the environment value, or with the empty
// string if unset. Validation then catches the resulting empty required field,
// which gives a better message than "unknown variable" would.
func expandEnv(raw []byte) []byte {
	return envPattern.ReplaceAllFunc(raw, func(match []byte) []byte {
		name := envPattern.FindSubmatch(match)[1]
		return []byte(os.Getenv(string(name)))
	})
}

// referencesEnv reports whether mqtt.password in the raw, unexpanded file was
// written as a ${VAR} reference rather than as a literal secret.
func referencesEnv(raw []byte) bool {
	var probe struct {
		MQTT struct {
			Password string `yaml:"password"`
		} `yaml:"mqtt"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return envPattern.MatchString(probe.MQTT.Password)
}

// Load reads, defaults and validates the configuration at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse defaults and validates an in-memory configuration. Load is the usual
// entry point; Parse exists so that tests need no temporary files.
func Parse(raw []byte) (*Config, error) {
	cfg := Defaults()
	if err := yaml.Unmarshal(expandEnv(raw), cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.passwordFromEnv = referencesEnv(raw)
	cfg.applyDerivedDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Defaults returns a configuration that needs only an MQTT host and one
// allowed network to be usable.
func Defaults() *Config {
	return &Config{
		App:  App{DeviceID: "auto"},
		Poll: Poll{DetectSeconds: 2, NetworkSeconds: 10, HeartbeatSeconds: 60},
		Detection: Detection{
			ActiveThreshold:      0.70,
			InactiveThreshold:    0.30,
			EnterDebounceSeconds: 2,
			ExitDebounceSeconds:  8,
		},
		MQTT: MQTT{
			Port:      1883,
			QoS:       1,
			Retain:    true,
			Discovery: Discovery{Enabled: true, Prefix: "homeassistant"},
		},
		Topics: Topics{
			State:        "desktop-presence/{device_id}/call",
			Availability: "desktop-presence/{device_id}/availability",
		},
		Detectors: map[string]Detector{
			"teams": {Enabled: true},
			"zoom":  {Enabled: true},
			"slack": {Enabled: true},
		},
		Logging: Logging{Level: "info"},
	}
}

// applyDerivedDefaults fills in values that depend on the host or on other
// fields, and can therefore only be computed after parsing.
func (c *Config) applyDerivedDefaults() {
	if c.App.DeviceID == "" || strings.EqualFold(c.App.DeviceID, "auto") {
		c.App.DeviceID = autoDeviceID()
	} else {
		// A hand-written device_id goes into MQTT topics and the Home
		// Assistant entity id, so it gets the same treatment as a derived one.
		c.App.DeviceID = slugify(c.App.DeviceID)
	}
	if c.MQTT.ClientID == "" {
		c.MQTT.ClientID = "callmqtt-" + c.App.DeviceID
	}
	if c.Logging.File == "" {
		if dir, err := os.UserConfigDir(); err == nil {
			c.Logging.File = filepath.Join(dir, "callmqtt", "callmqtt.log")
		}
	}
	c.Topics.State = c.resolveTopic(c.Topics.State)
	c.Topics.Availability = c.resolveTopic(c.Topics.Availability)
}

func (c *Config) resolveTopic(topic string) string {
	return strings.ReplaceAll(topic, "{device_id}", c.App.DeviceID)
}

// AutoDeviceID reports the identifier this machine would use when device_id
// is "auto". Exposed so `callmqtt init` can tell the user which machine the
// topics and Home Assistant entity will belong to.
func AutoDeviceID() string { return autoDeviceID() }

// autoDeviceID derives a topic-safe identifier from the hostname.
func autoDeviceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown-host"
	}
	return slugify(host)
}

// slugify reduces a string to lowercase alphanumerics and single hyphens, so
// it is safe to embed in an MQTT topic and in a Home Assistant entity id.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			// Collapse any run of separators into a single hyphen.
			if cur := b.String(); cur != "" && !strings.HasSuffix(cur, "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Validate reports every problem with the configuration at once.
func (c *Config) Validate() error {
	var problems []error
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	if c.MQTT.Host == "" {
		add("mqtt.host is required")
	}
	if c.MQTT.Port < 1 || c.MQTT.Port > 65535 {
		add("mqtt.port %d is out of range 1-65535", c.MQTT.Port)
	}
	if c.MQTT.QoS > 2 {
		add("mqtt.qos %d is out of range 0-2", c.MQTT.QoS)
	}
	if c.MQTT.Discovery.Enabled && c.MQTT.Discovery.Prefix == "" {
		add("mqtt.discovery.prefix is required when discovery is enabled")
	}

	if c.Topics.State == "" {
		add("topics.state is required")
	}
	if c.Topics.Availability == "" {
		add("topics.availability is required")
	}
	if c.Topics.State != "" && c.Topics.State == c.Topics.Availability {
		add("topics.state and topics.availability must differ")
	}

	// An empty allow-list denies everything. That is the safe direction to
	// fail, but in a file the user wrote by hand it is almost always a
	// mistake, so it is reported rather than silently honoured.
	if len(c.AllowedNetworks) == 0 {
		add("allowed_networks is empty, so nothing would ever be published")
	}
	for i, rule := range c.AllowedNetworks {
		label := rule.Name
		if label == "" {
			label = fmt.Sprintf("#%d", i)
			add("allowed_networks[%d].name is required", i)
		}
		if len(rule.SSIDs)+len(rule.BSSIDs)+len(rule.CIDRs)+len(rule.Gateways) == 0 {
			add("allowed_networks %q has no ssids, bssids, cidrs or gateways to match on", label)
		}
		for _, cidr := range rule.CIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				add("allowed_networks %q: invalid cidr %q", label, cidr)
			}
		}
		for _, gw := range rule.Gateways {
			if _, err := netip.ParseAddr(gw); err != nil {
				add("allowed_networks %q: invalid gateway address %q", label, gw)
			}
		}
	}

	if c.Detection.ActiveThreshold <= 0 || c.Detection.ActiveThreshold > 1 {
		add("detection.active_threshold %v must be within (0, 1]", c.Detection.ActiveThreshold)
	}
	if c.Detection.InactiveThreshold < 0 || c.Detection.InactiveThreshold >= 1 {
		add("detection.inactive_threshold %v must be within [0, 1)", c.Detection.InactiveThreshold)
	}
	if c.Detection.InactiveThreshold >= c.Detection.ActiveThreshold {
		add("detection.inactive_threshold %v must be below active_threshold %v",
			c.Detection.InactiveThreshold, c.Detection.ActiveThreshold)
	}
	if c.Detection.EnterDebounceSeconds < 0 {
		add("detection.enter_debounce_seconds must not be negative")
	}
	if c.Detection.ExitDebounceSeconds < 0 {
		add("detection.exit_debounce_seconds must not be negative")
	}

	if c.Poll.DetectSeconds < 1 {
		add("poll.detect_seconds must be at least 1")
	}
	if c.Poll.NetworkSeconds < 1 {
		add("poll.network_seconds must be at least 1")
	}
	if c.Poll.HeartbeatSeconds < 1 {
		add("poll.heartbeat_seconds must be at least 1")
	}

	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "error":
	default:
		add("logging.level %q must be one of debug, info, warn, error", c.Logging.Level)
	}

	if len(c.EnabledDetectors()) == 0 {
		add("no detectors are enabled, so no call could ever be detected")
	}

	return errors.Join(problems...)
}

// EnabledDetectors returns the names of the detectors the user has switched on.
func (c *Config) EnabledDetectors() []string {
	names := make([]string, 0, len(c.Detectors))
	for name, d := range c.Detectors {
		if d.Enabled {
			names = append(names, name)
		}
	}
	return names
}

// PasswordIsLiteral reports whether the MQTT password was written directly into
// the config file rather than referenced from the environment. Callers warn on
// this; it is not an error, because it is a defensible choice on a
// single-user machine.
func (c *Config) PasswordIsLiteral() bool {
	return c.MQTT.Password != "" && !c.passwordFromEnv
}

// Redacted returns a copy of the configuration that is safe to print or log.
func (c *Config) Redacted() Config {
	clone := *c
	if clone.MQTT.Password != "" {
		clone.MQTT.Password = "********"
	}
	return clone
}
