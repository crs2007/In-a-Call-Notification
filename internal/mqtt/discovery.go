package mqtt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eclipse/paho.golang/autopaho"

	"github.com/crs2007/callmqtt/internal/config"
)

// expireAfterSeconds is the last line of defence against a light left on.
//
// If Home Assistant hears nothing on the state topic for this long it marks
// the entity unavailable, which turns the light off. It must comfortably
// exceed the heartbeat interval or a healthy agent would be declared dead;
// heartbeatSafetyFactor sets that margin.
const heartbeatSafetyFactor = 1.5

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
		ValueTemplate:     "{{ value_json.state }}",
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
			Name:         "CallMQTT " + cfg.App.DeviceID,
			Manufacturer: "CallMQTT",
			Model:        "Desktop call presence",
			SWVersion:    version,
		},
		Origin: origin{
			Name:       "callmqtt",
			SWVersion:  version,
			SupportURL: "https://github.com/crs2007/callmqtt",
		},
	}
}

// publishDiscovery registers the entity with Home Assistant. It runs on every
// connection, not just the first, so that a broker which lost its retained
// messages gets them back.
func (c *Client) publishDiscovery(ctx context.Context, cm *autopaho.ConnectionManager) error {
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
