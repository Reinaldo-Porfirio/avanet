package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/buildinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/config"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/instance"
	"github.com/Reinaldo-Porfirio/avanet/internal/logging"
	"github.com/Reinaldo-Porfirio/avanet/internal/netinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/notify"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/report"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
	"github.com/Reinaldo-Porfirio/avanet/internal/tray"
	"github.com/Reinaldo-Porfirio/avanet/internal/wifi"
)

func runAgent(ctx context.Context, args []string, env Env) (err error) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // usage is printed by Main
	withTray := flags.Bool("tray", false, "ícone na bandeja")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%w: run takes no arguments", errUsage)
	}

	cfg, err := prepareDataDir(env)
	if err != nil {
		return err
	}

	lock, err := instance.Acquire(instanceName(cfg))
	if errors.Is(err, instance.ErrAlreadyRunning) {
		fmt.Fprintln(env.Stderr, "O AvaNet já está em execução (procure o ícone perto do relógio do Windows).")
		return errReported
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()

	logger, logFile, err := logging.New(logging.Options{Dir: cfg.LogDir(), Level: cfg.LogLevel})
	if err != nil {
		return fmt.Errorf("start logging: %w", err)
	}
	defer func() {
		if closeErr := logFile.Close(); closeErr != nil {
			fmt.Fprintf(env.Stderr, "aviso: falha ao fechar o log: %v\n", closeErr)
		}
	}()

	db, err := storage.Open(ctx, cfg.DatabasePath())
	if err != nil {
		if ctx.Err() == nil { // an interruption (Ctrl+C) is not a failure
			logger.Error("open database failed", "error", err)
		}
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("close database failed", "error", closeErr)
		}
	}()

	profile, err := loadProfile(ctx, db, logger)
	if err != nil {
		return err
	}
	if err := setupNotifications(ctx); err != nil && ctx.Err() == nil {
		logger.Warn("notifications will not be displayed", "error", err)
	}

	reports := &report.Service{
		Store: db, Notifier: notify.Toast{}, Logger: logger,
		Profile: func(ctx context.Context) (domain.Profile, error) { return loadProfile(ctx, db, logger) },
	}
	agentCfg := agent.Config{Profile: profile, Logger: logger, Reports: reports}
	if !*withTray {
		agentCfg.OnRound = consolePrinter(env.Stdout)
	}
	ag, err := agent.New(&agentCfg, realSources(logger, db), db)
	if err != nil {
		return err
	}

	logger.Info("agent started", "version", buildinfo.Version(), "data_dir", cfg.DataDir, "tray", *withTray)
	if !*withTray {
		fmt.Fprintf(env.Stdout, "AvaNet %s em execução (perfil %s). Dados em %s. Ctrl+C para sair.\n\n",
			buildinfo.Version(), profile.Name, cfg.DataDir)
		return ag.Run(ctx)
	}
	return runWithTray(ctx, ag, logger)
}

// runWithTray runs the agent in the background while the tray owns this
// goroutine; whichever stops first (agent error, "Sair", Ctrl+C) stops both.
func runWithTray(ctx context.Context, ag *agent.Agent, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var agentErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		agentErr = ag.Run(ctx)
		cancel() // close the tray if the agent stops on its own
	})

	if tray.Run(ctx, ag, logger) {
		logger.Info("quit requested from the tray")
	}
	cancel()
	wg.Wait()
	return agentErr
}

func realSources(logger *slog.Logger, db *storage.DB) agent.Sources {
	reader := netinfo.Reader{Logger: logger}
	return agent.Sources{
		Network: reader.Current,
		WiFi:    wifi.Current,
		Latency: probe.Latency{Echoer: probe.ICMPEchoer{}, Options: probe.DefaultEchoOptions}.Run,
		// Mobile data stays off for the agent until the panel (Phase 6) lets
		// the user turn it on.
		Speed: newSpeedRunner(reader, db, false),
	}
}

// instanceName allows one agent per data directory: the real one and a
// development copy (AVANET_DATA_DIR) can run side by side.
func instanceName(cfg config.Config) string {
	sum := sha256.Sum256([]byte(strings.ToLower(cfg.DataDir)))
	return "AvaNet-agent-" + hex.EncodeToString(sum[:8])
}

// loadProfile reads the chosen profile, falling back to the default when
// none was chosen or the stored one no longer exists.
func loadProfile(ctx context.Context, db *storage.DB, logger *slog.Logger) (domain.Profile, error) {
	id, found, err := db.Setting(ctx, storage.SettingProfile)
	if err != nil {
		return domain.Profile{}, err
	}
	if found {
		if p, ok := domain.ProfileByID(domain.ProfileID(id)); ok {
			return p, nil
		}
		logger.Warn("unknown stored profile; using the default", "profile", id)
	}
	p, _ := domain.ProfileByID(domain.DefaultProfile)
	return p, nil
}

// consolePrinter writes one line per round, and the speed test results, for
// `avanet run` in a terminal.
func consolePrinter(w io.Writer) func(agent.Status) {
	var (
		lastSpeed  *domain.SpeedResult
		lastNotice string
	)
	return func(s agent.Status) {
		line := s.At.Format("15:04:05") + "  "
		switch {
		case s.Outage:
			line += "sem conexão com a internet"
		case s.BestTarget == "":
			line += "internet não respondeu"
		default:
			line += "internet " + ptbr.Millis(s.Internet.Median) + " (" + s.BestTarget + ") · jitter " +
				ptbr.Millis(s.Internet.Jitter) + " · perda " + ptbr.Percent(s.Internet.LossPercent())
		}
		fmt.Fprintln(w, line)

		if s.LastSpeed != nil && s.LastSpeed != lastSpeed {
			lastSpeed = s.LastSpeed
			fmt.Fprintf(w, "          velocidade %s ↓ / %s ↑ · %s usados\n",
				ptbr.Mbps(s.LastSpeed.DownloadMbps), ptbr.Mbps(s.LastSpeed.UploadMbps), ptbr.MB(s.LastSpeed.BytesUsed()))
		}
		if s.SpeedNotice != "" && s.SpeedNotice != lastNotice {
			fmt.Fprintln(w, "          "+s.SpeedNotice)
		}
		lastNotice = s.SpeedNotice
	}
}
