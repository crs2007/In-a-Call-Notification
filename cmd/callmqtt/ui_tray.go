//go:build tray

package main

import (
	"context"
	"log/slog"

	"github.com/crs2007/callmqtt/internal/supervisor"
	"github.com/crs2007/callmqtt/internal/tray"
)

// runUI shows the tray icon and blocks until the user quits it. Startup is
// left nil until the autostart adapter (T31) lands; the tray already omits
// that menu item gracefully without one. brokerDialog is itself a stub
// outside Windows, so this still degrades correctly on other platforms.
func runUI(_ context.Context, sup *supervisor.Supervisor, log *slog.Logger, configPath, logPath string) error {
	return tray.Run(tray.Options{
		Supervisor: sup,
		Logger:     log,
		ConfigPath: configPath,
		LogPath:    logPath,
		Dialog:     brokerDialog,
	})
}
