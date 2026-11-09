package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/autostart"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/instance"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

// trayExecutable is the windowless build that Windows starts at logon
// (built with -ldflags -H=windowsgui; see cmd/avanetw).
const trayExecutable = "avanetw.exe"

// withDB opens the database for a short command and closes it afterwards.
func withDB(ctx context.Context, env Env, fn func(*storage.DB) error) (err error) {
	cfg, err := prepareDataDir(env)
	if err != nil {
		return err
	}
	db, err := storage.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return fn(db)
}

func runProfile(ctx context.Context, args []string, env Env) error {
	if len(args) > 1 {
		return fmt.Errorf("%w: profile takes at most one argument", errUsage)
	}
	return withDB(ctx, env, func(db *storage.DB) error {
		if len(args) == 1 {
			p, ok := domain.ProfileByID(domain.ProfileID(strings.ToLower(args[0])))
			if !ok {
				return fmt.Errorf("%w: unknown profile %q (see `avanet profile`)", errUsage, args[0])
			}
			if err := db.SetSetting(ctx, storage.SettingProfile, string(p.ID), time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "Perfil alterado para %s.\n", p.Name)
			fmt.Fprintln(env.Stdout, "Se o AvaNet estiver em execução, feche e abra de novo para aplicar.")
			return nil
		}

		current, _, err := db.Setting(ctx, storage.SettingProfile)
		if err != nil {
			return err
		}
		if current == "" {
			current = string(domain.DefaultProfile)
		}
		printProfiles(env.Stdout, domain.ProfileID(current))
		return nil
	})
}

func printProfiles(w io.Writer, current domain.ProfileID) {
	fmt.Fprintln(w, "Perfis de uso (escolha com: avanet profile <id>)")
	profiles := domain.Profiles()
	for i := range profiles {
		p := &profiles[i]
		marker := "  "
		if p.ID == current {
			marker = "▸ "
		}
		fmt.Fprintf(w, "\n%s%s (%s)\n", marker, p.Name, p.ID)
		fmt.Fprintf(w, "    %s\n", p.Description)
		fmt.Fprintf(w, "    Mede a cada %s; teste de velocidade a cada %s, dentro do limite de dados.\n",
			ptbr.Interval(p.LightInterval), ptbr.Interval(p.SpeedInterval))
	}
}

func runAutostart(args []string, env Env) error {
	entry := autostart.Default()
	switch {
	case len(args) == 0:
		command, enabled, err := entry.Status()
		if err != nil {
			return err
		}
		if enabled {
			fmt.Fprintf(env.Stdout, "Inicia com o Windows: sim\n  %s\n", command)
		} else {
			fmt.Fprintln(env.Stdout, "Inicia com o Windows: não (ative com: avanet autostart on)")
		}
		return nil
	case len(args) == 1 && args[0] == "on":
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate executable: %w", err)
		}
		exe := filepath.Join(filepath.Dir(self), trayExecutable)
		if _, err := os.Stat(exe); err != nil {
			fmt.Fprintf(env.Stderr, "Não encontrei %s ao lado deste programa (%s).\n", trayExecutable, filepath.Dir(self))
			fmt.Fprintln(env.Stderr, "Ele é a versão sem janela que o Windows abre ao ligar; veja o README para gerá-lo.")
			return errReported
		}
		if err := entry.Enable(exe); err != nil {
			return err
		}
		fmt.Fprintln(env.Stdout, "Pronto: o AvaNet vai iniciar junto com o Windows, com o ícone perto do relógio.")
		fmt.Fprintln(env.Stdout, "Dá para desligar aqui (avanet autostart off) ou no Gerenciador de Tarefas > Aplicativos de inicialização.")
		return nil
	case len(args) == 1 && args[0] == "off":
		if err := entry.Disable(); err != nil {
			return err
		}
		fmt.Fprintln(env.Stdout, "O AvaNet não vai mais iniciar com o Windows.")
		return nil
	default:
		return fmt.Errorf("%w: autostart takes \"on\", \"off\" or nothing", errUsage)
	}
}

func runStatus(ctx context.Context, args []string, env Env) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: status takes no arguments", errUsage)
	}
	cfg, err := prepareDataDir(env)
	if err != nil {
		return err
	}
	running := "parado"
	switch lock, err := instance.Acquire(instanceName(cfg)); {
	case errors.Is(err, instance.ErrAlreadyRunning):
		running = "em execução"
	case err != nil:
		return err
	default:
		if err := lock.Release(); err != nil {
			return err
		}
	}

	return withDB(ctx, env, func(db *storage.DB) error {
		now := time.Now()
		profileID, _, err := db.Setting(ctx, storage.SettingProfile)
		if err != nil {
			return err
		}
		profile, ok := domain.ProfileByID(domain.ProfileID(profileID))
		if !ok {
			profile, _ = domain.ProfileByID(domain.DefaultProfile)
		}
		coverage, err := db.Coverage(ctx, domain.StartOfLocalDay(now), now)
		if err != nil {
			return err
		}
		fixed, err := db.DataUsage(ctx, domain.DayOf(now), domain.DataFixed)
		if err != nil {
			return err
		}
		mobile, err := db.DataUsage(ctx, domain.DayOf(now), domain.DataMobile)
		if err != nil {
			return err
		}
		_, autostartOn, autostartErr := autostart.Default().Status()

		tw := newTable(env.Stdout)
		fmt.Fprintf(tw, "Monitoramento\t%s · perfil %s\n", running, profile.Name)
		switch {
		case autostartErr != nil:
			fmt.Fprintf(tw, "Iniciar com o Windows\tnão disponível\n")
		case autostartOn:
			fmt.Fprintf(tw, "Iniciar com o Windows\tsim\n")
		default:
			fmt.Fprintf(tw, "Iniciar com o Windows\tnão\n")
		}
		fmt.Fprintf(tw, "Monitorado hoje\t%s de %s\n", ptbr.Hours(coverage), ptbr.Hours(now.Sub(domain.StartOfLocalDay(now))))
		budget := domain.DefaultDataBudget()
		usage := fmt.Sprintf("%s de %s (rede fixa)", ptbr.MB(fixed), ptbr.MB(budget.Fixed.DailyBytes))
		if mobile > 0 {
			usage += fmt.Sprintf(" · %s de %s (dados móveis)", ptbr.MB(mobile), ptbr.MB(budget.Mobile.DailyBytes))
		}
		fmt.Fprintf(tw, "Dados em testes hoje\t%s\n", usage)

		if err := statusLatest(ctx, db, tw); err != nil {
			return err
		}
		flush(tw)
		return nil
	})
}

func statusLatest(ctx context.Context, db *storage.DB, w io.Writer) error {
	at, samples, found, err := db.LatestRound(ctx)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintf(w, "Última medição\tnenhuma ainda (rode: avanet run)\n")
	} else {
		parts := make([]string, 0, len(samples))
		for _, s := range samples {
			name := s.Target
			if name == domain.GatewayTarget {
				name = "roteador"
			}
			value := "sem resposta"
			if s.Stats.Reachable() {
				value = ptbr.Millis(s.Stats.Median)
			}
			parts = append(parts, name+" "+value)
		}
		fmt.Fprintf(w, "Última medição\t%s · %s\n", at.Format("02/01 15:04"), strings.Join(parts, " · "))
	}

	r, trigger, found, err := db.LatestSpeedTest(ctx)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintf(w, "Último teste de velocidade\tnenhum ainda\n")
		return nil
	}
	origin := map[storage.SpeedTrigger]string{
		storage.TriggerScheduled: "agendado", storage.TriggerAdaptive: "extra, por piora", storage.TriggerManual: "manual",
	}[trigger]
	fmt.Fprintf(w, "Último teste de velocidade\t%s · %s ↓ / %s ↑ (%s)\n",
		r.Start.Format("02/01 15:04"), ptbr.Mbps(r.DownloadMbps), ptbr.Mbps(r.UploadMbps), origin)
	return nil
}
