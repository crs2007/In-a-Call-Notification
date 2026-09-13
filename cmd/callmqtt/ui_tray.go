//go:build tray

package main

import (
	"context"
	"log/slog"

	"github.com/crs2007/callmqtt/internal/supervisor"
	"github.com/crs2007/callmqtt/internal/tray"
)

// runUI shows the tray icon and blocks until the user quits it. Dialog and
// Startup are left nil until the Windows dialog and autostart adapters land;
// the tray already degrades those menu items gracefully without them.
func runUI(_ context.Context, sup *supervisor.Supervisor, log *slog.Logger, configPath, logPath string) error {
	return tray.Run(tray.Options{
		Supervisor: sup,
		Logger:     log,
		ConfigPath: configPath,
		LogPath:    logPath,
	})
}
