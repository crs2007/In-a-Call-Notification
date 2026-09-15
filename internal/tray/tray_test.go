//go:build tray

package tray

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"

	"github.com/crs2007/callmqtt/internal/engine"
	"github.com/crs2007/callmqtt/internal/model"
)

func TestIconPNG(t *testing.T) {
	for _, icon := range []trayIconState{
		{color: colorGray},
		{color: colorGreen, brokerConnected: true},
		{color: colorYellow},
		{color: colorRed, brokerConnected: true},
	} {
		data := iconPNG(icon)
		if len(data) == 0 {
			t.Fatalf("empty png for %+v", icon)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode png for %+v: %v", icon, err)
		}
		if img.Bounds().Dx() != 22 || img.Bounds().Dy() != 22 {
			t.Fatalf("expected 22x22, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
		}

		got := color.RGBAModel.Convert(img.At(17, 17)).(color.RGBA)
		want := color.RGBA{R: 239, G: 68, B: 68, A: 255}
		if icon.brokerConnected {
			want = color.RGBA{R: 34, G: 197, B: 94, A: 255}
		}
		if got != want {
			t.Fatalf("badge for %+v = %+v, want %+v", icon, got, want)
		}
	}
}

func TestStatusTooltip(t *testing.T) {
	tests := []struct {
		name      string
		status    engine.Status
		connected bool
		want      string
	}{
		{"active and connected", engine.Status{State: model.StateActive, App: "teams"}, true, "In a Call Notification | On call: yes (Teams) | MQTT: connected"},
		{"active and disconnected", engine.Status{State: model.StateActive, App: "teams"}, false, "In a Call Notification | On call: yes (Teams) | MQTT: disconnected"},
		{"inactive", engine.Status{State: model.StateInactive}, true, "In a Call Notification | On call: not on a call | MQTT: connected"},
		{"paused", engine.Status{State: model.StateActive, Paused: true}, false, "In a Call Notification | On call: detection paused | MQTT: disconnected"},
		{"starting", engine.Status{State: model.StateUnknown}, false, "In a Call Notification | On call: starting | MQTT: disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusTooltip(tt.status, tt.connected); got != tt.want {
				t.Fatalf("statusTooltip() = %q, want %q", got, tt.want)
			}
		})
	}
}
