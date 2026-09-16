//go:build tray

package main

import (
	"context"
	"log/slog"

	"github.com/crs2007/callmqtt/internal/supervisor"
	"github.com/crs2007/callmqtt/internal/tray"
)

// runUI shows the tray icon and blocks until the user quits it or ctx is
// cancelled (SIGINT/SIGTERM from a console). brokerDialog and startup are
// both stubs outside Windows, so this still degrades correctly on other
// platforms.
func runUI(ctx context.Context, sup *supervisor.Supervisor, log *slog.Logger, configPath, logPath string) error {
	return tray.Run(ctx, tray.Options{
		Supervisor: sup,
		Logger:     log,
		ConfigPath: configPath,
		LogPath:    logPath,
		Dialog:     brokerDialog,
		Startup:    startup,
	})
}
