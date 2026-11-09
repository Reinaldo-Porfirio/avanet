// Package tray shows AvaNet in the Windows notification area: an icon
// colored by the connection's current quality and a small menu.
package tray

import (
	"context"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"

	"fyne.io/systray"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// refreshEvery is how often the icon and menu follow the agent's status.
const refreshEvery = 5 * time.Second

// Controller is what the tray needs from the agent.
type Controller interface {
	Status() agent.Status
	RequestSpeedTest()
}

// Run shows the tray icon until ctx is cancelled or the user chooses "Sair".
// It reports whether the user asked to quit. openPanel is nil when the panel
// is not available, which disables its menu item.
//
// Windows delivers the icon's messages to the thread that created it, so Run
// locks itself to its OS thread for its whole duration.
func Run(ctx context.Context, ctrl Controller, log *slog.Logger, openPanel func()) (quitByUser bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var byUser atomic.Bool
	systray.Run(func() { onReady(ctx, ctrl, log, openPanel, &byUser) }, func() {})
	return byUser.Load()
}

func onReady(ctx context.Context, ctrl Controller, log *slog.Logger, openPanel func(), byUser *atomic.Bool) {
	icons := make(map[domain.Quality][]byte, len(qualityColors))
	for q := range qualityColors {
		data, err := icon(q)
		if err != nil {
			log.Error("build tray icon failed", "quality", q, "error", err)
			continue
		}
		icons[q] = data
	}

	systray.SetTitle("AvaNet")
	panelItem := systray.AddMenuItem("Abrir painel", "Boletins, situação agora e ajustes")
	if openPanel == nil {
		panelItem.Disable()
	}
	systray.AddSeparator()
	statusItem := systray.AddMenuItem("", "")
	statusItem.Disable()
	speedItem := systray.AddMenuItem("", "")
	speedItem.Disable()
	systray.AddSeparator()
	speedNow := systray.AddMenuItem("Testar velocidade agora", "Mede download e upload (respeita o orçamento de dados)")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Sair do AvaNet", "Para o monitoramento até a próxima inicialização")

	shown := domain.Quality(-1)
	refresh := func() {
		s := ctrl.Status()
		if s.Quality != shown {
			if data, ok := icons[s.Quality]; ok {
				systray.SetIcon(data)
				shown = s.Quality
			}
		}
		systray.SetTooltip(tooltip(&s))
		statusItem.SetTitle(statusLine(&s))
		speedItem.SetTitle(speedLine(&s))
	}
	refresh()

	go func() {
		ticker := time.NewTicker(refreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				systray.Quit()
				return
			case <-quit.ClickedCh:
				byUser.Store(true)
				systray.Quit()
				return
			case <-panelItem.ClickedCh:
				if openPanel != nil {
					openPanel()
				}
			case <-speedNow.ClickedCh:
				ctrl.RequestSpeedTest()
				speedItem.SetTitle("Teste de velocidade solicitado…")
			case <-ticker.C:
				refresh()
			}
		}
	}()
}
