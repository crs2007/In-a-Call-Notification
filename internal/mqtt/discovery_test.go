package mqtt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/crs2007/callmqtt/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(`
allowed_networks:
  - name: Home
    cidrs: ["192.168.1.0/24"]
app:
  device_id: sharon-pc
mqtt:
  host: 192.168.1.10
`))
	if err != nil {
		t.Fatalf("test config: %v", err)
	}
	return cfg
}

func TestDiscoveryTopic(t *testing.T) {
	got := DiscoveryTopic(testConfig(t))
	want := "homeassistant/binary_sensor/sharon-pc/call/config"
	if got != want {
		t.Errorf("DiscoveryTopic() = %q, want %q", got, want)
	}
}

// The discovery payload is a public contract: a change here silently breaks
// every Home Assistant automation built on the entity. Pin the whole thing.
func TestDiscoveryPayloadIsStable(t *testing.T) {
	body, err := json.MarshalIndent(BuildDiscovery(testConfig(t), "1.2.3"), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want := `{
  "name": "Call",
  "unique_id": "callmqtt_sharon-pc_call",
  "object_id": "callmqtt_sharon-pc_call",
  "device_class": "sound",
  "state_topic": "desktop-presence/sharon-pc/call",
  "value_template": "{{ value_json.state }}",
  "payload_on": "active",
  "payload_off": "inactive",
  "availability_topic": "desktop-presence/sharon-pc/availability",
  "payload_available": "online",
  "payload_not_available": "offline",
  "expire_after": 90,
  "json_attributes_topic": "desktop-presence/sharon-pc/call",
  "icon": "mdi:phone-in-talk",
  "device": {
    "identifiers": [
      "callmqtt_sharon-pc"
    ],
    "name": "CallMQTT sharon-pc",
    "manufacturer": "CallMQTT",
    "model": "Desktop call presence",
    "sw_version": "1.2.3"
  },
  "origin": {
    "name": "callmqtt",
    "sw_version": "1.2.3",
    "support_url": "https://github.com/crs2007/callmqtt"
  }
}`

	if string(body) != want {
		t.Errorf("discovery payload changed.\n got:\n%s\n\nwant:\n%s", body, want)
	}
}

// expire_after is what turns the light off when the agent goes silent without
// a chance to say goodbye. It must outlast the heartbeat, or a perfectly
// healthy agent would be declared dead between beats.
func TestExpireAfterOutlastsHeartbeat(t *testing.T) {
	for _, heartbeat := range []int{10, 30, 60, 120} {
		cfg := testConfig(t)
		cfg.Poll.HeartbeatSeconds = heartbeat

		got := BuildDiscovery(cfg, "test").ExpireAfter
		if got <= heartbeat {
			t.Errorf("heartbeat %ds gives expire_after %ds, which would mark a healthy agent unavailable", heartbeat, got)
		}
	}
}

// The payload is the only thing that leaves the machine. Nothing resembling a
// meeting name may appear in it.
func TestStatePayloadCarriesNoMeetingDetail(t *testing.T) {
	body, err := json.Marshal(Payload{
		Device:     "sharon-pc",
		State:      "active",
		App:        "teams",
		Apps:       []string{"teams", "zoom"},
		Confidence: 0.85,
		Network:    "Home",
		Timestamp:  time.Date(2026, 9, 13, 11, 45, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want := `{"device":"sharon-pc","state":"active","app":"teams","apps":["teams","zoom"],"confidence":0.85,"network":"Home","timestamp":"2026-09-13T11:45:30Z"}`
	if string(body) != want {
		t.Errorf("state payload changed.\n got: %s\nwant: %s", body, want)
	}

	// The struct has no field that could carry one, and this test fails loudly
	// if someone adds one.
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allowed := map[string]bool{
		"device": true, "state": true, "app": true, "apps": true,
		"confidence": true, "network": true, "timestamp": true,
	}
	for name := range fields {
		if !allowed[name] {
			t.Errorf("unexpected field %q in the published payload: everything here leaves the machine", name)
		}
	}
}

// An inactive payload omits the app entirely rather than naming the last one.
func TestInactivePayloadOmitsApp(t *testing.T) {
	body, err := json.Marshal(Payload{
		Device:    "sharon-pc",
		State:     "inactive",
		Timestamp: time.Date(2026, 9, 13, 12, 32, 10, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"app"`) {
		t.Errorf("inactive payload should omit app, got %s", body)
	}
}
