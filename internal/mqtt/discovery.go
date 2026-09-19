package mqtt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/crs2007/callmqtt/internal/config"
	"github.com/crs2007/callmqtt/internal/model"
)

// expire_after (below) is the last line of defence against a light left on.
//
// If Home Assistant hears nothing on the state topic for this long it marks
// the entity unavailable, which turns the light off. It must comfortably
// exceed the heartbeat interval or a healthy agent would be declared dead;
// config.HeartbeatExpireSafetyFactor sets that margin, and
// internal/config.Config.Validate additionally checks it against
// detect_seconds, so the two packages' notion of "comfortably exceeds" can't
// drift apart. That check exists specifically to protect the relationship
// this constant establishes here — see internal/config/config.go's Validate.
const heartbeatSafetyFactor = config.HeartbeatExpireSafetyFactor

// haPayloadNone is Home Assistant's PAYLOAD_NONE for MQTT binary sensors:
// a templated state equal to this string sets the entity to `unknown`
// rather than on or off. It is the only way to express "no opinion yet"
// to a binary_sensor, and it is what the discovery value_template turns
// the agent's own "unknown" state into.
const haPayloadNone = "None"

// valueTemplate is the discovery value_template. It picks the state field
// out of the JSON payload and maps model.StateUnknown to haPayloadNone;
// see the ValueTemplate comment in BuildDiscovery for why that mapping
// must not be simplified away.
const valueTemplate = "{{ '" + haPayloadNone + "' if value_json.state == '" + string(model.StateUnknown) + "' else value_json.state }}"

// discoveryConfig is the Home Assistant MQTT Discovery payload.
//
// Publishing this retained means the user never writes a line of Home
// Assistant YAML: the binary_sensor appears by itself, already wired to the
// availability topic, and is ready to drive an automation.
type discoveryConfig struct {
	Name              string `json:"name"`
	UniqueID          string `json:"unique_id"`
	ObjectID          string `json:"object_id"`
	DeviceClass       string `json:"device_class"`
	StateTopic        string `json:"state_topic"`
	ValueTemplate     string `json:"value_template"`
	PayloadOn         string `json:"payload_on"`
	PayloadOff        string `json:"payload_off"`
	AvailabilityTopic string `json:"availability_topic"`
	PayloadAvailable  string `json:"payload_available"`
	PayloadNotAvail   string `json:"payload_not_available"`
	ExpireAfter       int    `json:"expire_after"`
	JSONAttrTopic     string `json:"json_attributes_topic"`
	Icon              string `json:"icon"`
	Device            device `json:"device"`
	Origin            origin `json:"origin"`
}

type device struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	SWVersion    string   `json:"sw_version"`
}

type origin struct {
	Name       string `json:"name"`
	SWVersion  string `json:"sw_version"`
	SupportURL string `json:"support_url"`
}

// DiscoveryTopic returns the retained topic the entity config is published to.
func DiscoveryTopic(cfg *config.Config) string {
	return fmt.Sprintf("%s/binary_sensor/%s/call/config", cfg.MQTT.Discovery.Prefix, cfg.App.DeviceID)
}

// BuildDiscovery returns the Home Assistant entity configuration.
func BuildDiscovery(cfg *config.Config, version string) discoveryConfig {
	expireAfter := int(float64(cfg.Poll.HeartbeatSeconds) * heartbeatSafetyFactor)

	return discoveryConfig{
		Name:        "Call",
		UniqueID:    "callmqtt_" + cfg.App.DeviceID + "_call",
		ObjectID:    "callmqtt_" + cfg.App.DeviceID + "_call",
		DeviceClass: "sound",
		StateTopic:  cfg.Topics.State,
		// The state topic carries JSON; Home Assistant needs the one field.
		//
		// The state field takes three values, not two: "active", "inactive"
		// and "unknown" (model.StateUnknown — what the agent publishes at
		// startup, before its state machine has decided anything). Home
		// Assistant's binary_sensor only knows payload_on, payload_off and
		// the literal "None", and a payload matching none of those is
		// *dropped* — it logs "No matching payload found", leaves the entity
		// in whatever state it last had, and still resets expire_after. So
		// without this mapping the startup "unknown" would silently fail to
		// clear a stale retained "active" left by a crash mid-call, and the
		// light would stay red for a whole exit debounce (issue #5). Mapping
		// it to "None" makes the entity read `unknown`, which the README's
		// automation template already treats as call ended.
		ValueTemplate:     valueTemplate,
		PayloadOn:         "active",
		PayloadOff:        "inactive",
		AvailabilityTopic: cfg.Topics.Availability,
		PayloadAvailable:  Online,
		PayloadNotAvail:   Offline,
		ExpireAfter:       expireAfter,
		// The same topic serves the attributes, so confidence, app and network
		// are visible on the entity without a second publish.
		JSONAttrTopic: cfg.Topics.State,
		Icon:          "mdi:phone-in-talk",
		Device: device{
			Identifiers:  []string{"callmqtt_" + cfg.App.DeviceID},
			Name:         "In a Call Notification " + cfg.App.DeviceID,
			Manufacturer: "In a Call Notification",
			Model:        "Desktop call presence",
			SWVersion:    version,
		},
		Origin: origin{
			Name:       "In a Call Notification",
			SWVersion:  version,
			SupportURL: "https://github.com/crs2007/In-a-Call-Notification",
		},
	}
}

// publishDiscovery registers the entity with Home Assistant. It runs on every
// connection, not just the first, so that a broker which lost its retained
// messages gets them back.
func (c *Client) publishDiscovery(ctx context.Context, cm connectionPublisher) error {
	if !c.cfg.MQTT.Discovery.Enabled {
		return nil
	}

	body, err := json.Marshal(BuildDiscovery(c.cfg, c.version))
	if err != nil {
		return fmt.Errorf("marshal discovery config: %w", err)
	}

	topic := DiscoveryTopic(c.cfg)
	if err := c.publish(ctx, cm, topic, body, true); err != nil {
		return err
	}

	c.log.Info("published home assistant discovery", "topic", topic)
	return nil
}
