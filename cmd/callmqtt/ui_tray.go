//go:build tray

package main

import (
	"context"
	"log/slog"

	"github.com/crs2007/callmqtt/internal/supervisor"
	"github.com/crs2007/callmqtt/internal/tray"
)

// runUI shows the tray icon and blocks until the user quits it or ctx is
// cancelled (SIGINT/SIGTERM from a console). Every platform hook is nil
// where the platform has no implementation — brokerDialog outside Windows,
// startup outside Windows and macOS, the permission item outside macOS — so
// the tray omits or falls back rather than offering a control that does
// nothing.
func runUI(ctx context.Context, sup *supervisor.Supervisor, log *slog.Logger, configPath, logPath string) error {
	var loginItem tray.Startup
	if startupSupported {
		loginItem = startup
	}
	return tray.Run(ctx, tray.Options{
		Supervisor: sup,
		Logger:     log,
		ConfigPath: configPath,
		LogPath:    logPath,
		Dialog:     brokerDialog,
		Startup:    loginItem,
		Permission: trayPermission(),
	})
}
