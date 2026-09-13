//go:build !tray

package main

import (
	"context"
	"log/slog"

	"github.com/crs2007/callmqtt/internal/supervisor"
)

// runUI has nothing to show in a headless build, so it just blocks until the
// context is cancelled — the same shutdown path the tray build takes when the
// user quits.
func runUI(ctx context.Context, _ *supervisor.Supervisor, _ *slog.Logger, _, _ string) error {
	<-ctx.Done()
	return nil
}
