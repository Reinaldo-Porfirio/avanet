package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/buildinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/netinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
	"github.com/Reinaldo-Porfirio/avanet/internal/wifi"
)

func runSpeedTest(ctx context.Context, args []string, env Env) (err error) {
	flags := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	allowMobile := flags.Bool("movel", false, "permite este teste em dados móveis")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%w: speedtest takes no arguments", errUsage)
	}

	cfg, err := prepareDataDir(env)
	if err != nil {
		return err
	}
	db, err := storage.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	logger := slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	reader := netinfo.Reader{Logger: logger}
	network, err := reader.Current()
	if err != nil {
		if explainSpeedError(env.Stderr, err) {
			return errReported
		}
		return fmt.Errorf("read network: %w", err)
	}

	runner := newSpeedRunner(reader, db, *allowMobile)
	outcome, err := runner.Measure(ctx, network, func(limits domain.TransferLimits) {
		fmt.Fprintf(env.Stdout, "Medindo download e upload (no máximo %s de dados)...\n\n", ptbr.MB(limits.Total()))
	})
	if explainSpeedError(env.Stderr, err) {
		return errReported
	}
	if err != nil {
		return err
	}

	if err := saveManualSpeedTest(ctx, db, network, &outcome.Result); err != nil {
		return err
	}
	renderSpeed(env.Stdout, network, &outcome)
	return nil
}

// newSpeedRunner wires the real speed test with the default budget. Mobile
// data needs explicit consent (`-movel` here; a setting in a later phase).
func newSpeedRunner(reader netinfo.Reader, usage agent.UsageStore, allowMobile bool) agent.SpeedRunner {
	budget := domain.DefaultDataBudget()
	budget.MobileEnabled = allowMobile
	return agent.SpeedRunner{
		Run:      probe.Speed{UserAgent: "AvaNet/" + buildinfo.Version()}.Run,
		Counters: reader.Counters,
		Usage:    usage,
		Budget:   budget,
		Now:      time.Now,
	}
}

func saveManualSpeedTest(ctx context.Context, db *storage.DB, network domain.Network, result *domain.SpeedResult) error {
	// The agent identifies Wi-Fi networks by name; do the same so manual and
	// automatic tests on the same network share its record.
	ssid := ""
	if network.Kind == domain.ConnectionWiFi {
		if info, err := wifi.Current(); err == nil {
			ssid = info.SSID
		}
	}
	netID, err := db.EnsureNetwork(ctx, network, ssid, time.Now())
	if err != nil {
		return err
	}
	return db.SaveSpeedTest(ctx, netID, storage.TriggerManual, result)
}

// explainSpeedError prints a Portuguese explanation for expected refusals and
// reports whether it did.
func explainSpeedError(w io.Writer, err error) bool {
	switch {
	case errors.Is(err, domain.ErrMobileDataDisabled):
		fmt.Fprintln(w, "Você está em dados móveis ou hotspot (conexão limitada). Para não gastar")
		fmt.Fprintln(w, "sua franquia, o teste de velocidade fica desligado nesse tipo de rede.")
		fmt.Fprintln(w, "Para testar mesmo assim (até 8 MB): avanet speedtest -movel")
		return true
	case errors.Is(err, domain.ErrBudgetExhausted):
		fmt.Fprintln(w, "O orçamento de dados de hoje para testes de velocidade nesta rede acabou.")
		fmt.Fprintln(w, "Ele é renovado à meia-noite.")
		return true
	case errors.Is(err, netinfo.ErrNoConnection):
		fmt.Fprintln(w, "Sem conexão com a internet: nenhuma interface de rede está ativa.")
		return true
	}
	return false
}

func renderSpeed(w io.Writer, network domain.Network, o *agent.SpeedOutcome) {
	r := &o.Result
	fmt.Fprintf(w, "Velocidade · %s · %s\n\n", r.Server, connectionKindName(network.Kind))

	tw := newTable(w)
	fmt.Fprintf(tw, "  Download\t%s\n", ptbr.Mbps(r.DownloadMbps))
	fmt.Fprintf(tw, "  Upload\t%s\n", ptbr.Mbps(r.UploadMbps))
	fmt.Fprintf(tw, "  Latência\t%s (com a conexão ociosa)\n", ptbr.Millis(r.IdleLatency))
	flush(tw)

	fmt.Fprintf(w, "\n  Dados usados neste teste: %s · hoje: %s de %s (%s)\n",
		ptbr.MB(r.BytesUsed()), ptbr.MB(o.UsedToday), ptbr.MB(o.DailyLimit), dataClassName(o.Class))

	if r.DownloadCapped {
		fmt.Fprintln(w, "\n  Observação: o download chegou ao teto de dados do teste antes de durar 1 s;")
		fmt.Fprintln(w, "  em conexões muito rápidas o valor real pode ser um pouco maior.")
	}
	if r.UploadCapped {
		fmt.Fprintln(w, "\n  Observação: o upload chegou ao teto de dados do teste antes de durar 1 s;")
		fmt.Fprintln(w, "  o valor real pode ser um pouco maior.")
	}
	if r.LocalTraffic {
		fmt.Fprintln(w, "\n  Atenção: outros programas usaram a rede durante o teste;")
		fmt.Fprintln(w, "  o resultado pode estar abaixo do que a sua conexão entrega.")
	}
}

func dataClassName(c domain.DataClass) string {
	if c == domain.DataMobile {
		return "dados móveis"
	}
	return "rede fixa"
}
