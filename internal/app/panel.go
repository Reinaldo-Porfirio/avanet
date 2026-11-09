package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/autostart"
	"github.com/Reinaldo-Porfirio/avanet/internal/web"
	"github.com/Reinaldo-Porfirio/avanet/ui"
)

const (
	// trayLinkTTL is short because the browser opens the link immediately.
	trayLinkTTL = time.Minute
	// consoleLinkTTL leaves time to copy the link from the terminal.
	consoleLinkTTL = time.Hour
)

// autostartControl lets the panel change the same setting as `avanet autostart`.
type autostartControl struct{}

func (autostartControl) Enabled() (bool, error) {
	_, enabled, err := autostart.Default().Status()
	return enabled, err
}

func (autostartControl) SetEnabled(on bool) error {
	entry := autostart.Default()
	if !on {
		return entry.Disable()
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	exe := filepath.Join(filepath.Dir(self), trayExecutable)
	if _, err := os.Stat(exe); err != nil || autostart.IsTemporaryBuild(exe) {
		return web.ErrAutostartUnavailable
	}
	return entry.Enable(exe)
}

// startPanel starts the local panel in the background. A panel that cannot
// start must never stop the monitoring, so failures are logged and the server
// comes back nil. The returned function stops it and waits.
func startPanel(ctx context.Context, deps *web.Deps, logger *slog.Logger) (server *web.Server, stop func()) {
	assets, err := ui.Assets()
	if err != nil {
		logger.Error("panel files unavailable", "error", err)
		return nil, func() {}
	}
	deps.Assets = assets

	server, err = web.New(deps)
	if err == nil {
		err = server.Listen(ctx)
	}
	if err != nil {
		if ctx.Err() == nil {
			logger.Error("panel could not start", "error", err)
		}
		return nil, func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := server.Serve(ctx); err != nil {
			logger.Error("panel stopped with an error", "error", err)
		}
	})
	return server, func() {
		cancel()
		wg.Wait()
	}
}

// panelOpener returns what the tray calls for "Abrir painel", or nil when the
// panel is not running.
func panelOpener(server *web.Server, logger *slog.Logger) func() {
	if server == nil {
		return nil
	}
	return func() {
		if err := web.OpenBrowser(server.OpenURL(trayLinkTTL)); err != nil {
			logger.Error("open panel failed", "error", err)
		}
	}
}
