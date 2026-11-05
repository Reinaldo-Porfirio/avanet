package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/buildinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/netinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

// accountingTimeout bounds the write that charges the data budget. It runs
// even after Ctrl+C: bytes already transferred must always be counted.
const accountingTimeout = 5 * time.Second

// speedSources are the speed test's dependencies, injectable for tests.
type speedSources struct {
	network  func() (domain.Network, error)
	counters func(index uint32) (domain.InterfaceCounters, error)
	run      func(ctx context.Context, limits domain.TransferLimits) (domain.SpeedResult, error)
	now      func() time.Time
}

// usageStore persists the daily data usage (implemented by *storage.DB).
type usageStore interface {
	DataUsage(ctx context.Context, day domain.Day, class domain.DataClass) (int64, error)
	AddDataUsage(ctx context.Context, day domain.Day, class domain.DataClass, bytes int64) error
}

type speedOutcome struct {
	network    domain.Network
	class      domain.DataClass
	result     domain.SpeedResult
	usedToday  int64 // including this test
	dailyLimit int64
}

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

	budget := domain.DefaultDataBudget()
	budget.MobileEnabled = *allowMobile // explicit, per-run consent

	logger := slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	reader := netinfo.Reader{Logger: logger}
	src := speedSources{
		network:  reader.Current,
		counters: reader.Counters,
		run:      probe.Speed{UserAgent: "AvaNet/" + buildinfo.Version()}.Run,
		now:      time.Now,
	}

	outcome, err := measureSpeed(ctx, src, db, budget, env.Stdout)
	if explained := explainSpeedError(env.Stderr, err); explained {
		return errReported
	}
	if err != nil {
		return err
	}
	renderSpeed(env.Stdout, &outcome)
	return nil
}

// measureSpeed checks the budget, runs the test, charges the bytes used and
// flags local traffic. progress receives a line before the test starts.
func measureSpeed(ctx context.Context, src speedSources, usage usageStore, budget domain.DataBudget, progress io.Writer) (speedOutcome, error) {
	if err := budget.Validate(); err != nil {
		return speedOutcome{}, err
	}
	network, err := src.network()
	if err != nil {
		return speedOutcome{}, fmt.Errorf("read network: %w", err)
	}

	out := speedOutcome{network: network, class: domain.ClassOf(network), dailyLimit: budget.Daily(domain.ClassOf(network))}
	day := domain.DayOf(src.now())
	used, err := usage.DataUsage(ctx, day, out.class)
	if err != nil {
		return speedOutcome{}, err
	}
	limits, err := budget.Plan(out.class, used)
	if err != nil {
		return speedOutcome{}, err
	}

	before, countersErr := src.counters(network.InterfaceIndex)

	fmt.Fprintf(progress, "Medindo download e upload (no máximo %s de dados)...\n\n", formatMB(limits.Total()))
	result, runErr := src.run(ctx, limits)

	if result.BytesUsed() > 0 {
		chargeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountingTimeout)
		chargeErr := usage.AddDataUsage(chargeCtx, day, out.class, result.BytesUsed())
		cancel()
		if chargeErr != nil {
			return speedOutcome{}, errors.Join(runErr, fmt.Errorf("charge data budget: %w", chargeErr))
		}
	}
	if runErr != nil {
		return speedOutcome{}, fmt.Errorf("speed test: %w", runErr)
	}

	if countersErr == nil {
		if after, err := src.counters(network.InterfaceIndex); err == nil {
			result.LocalTraffic = domain.LocalTrafficSuspected(before, after, result.BytesUsed())
		}
	}

	out.result = result
	out.usedToday = used + result.BytesUsed()
	return out, nil
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

func renderSpeed(w io.Writer, o *speedOutcome) {
	r := &o.result
	fmt.Fprintf(w, "Velocidade · %s · %s\n\n", r.Server, connectionKindName(o.network.Kind))

	tw := newTable(w)
	fmt.Fprintf(tw, "  Download\t%s\n", formatMbps(r.DownloadMbps))
	fmt.Fprintf(tw, "  Upload\t%s\n", formatMbps(r.UploadMbps))
	fmt.Fprintf(tw, "  Latência\t%s (com a conexão ociosa)\n", formatMillis(r.IdleLatency))
	flush(tw)

	className := "rede fixa"
	if o.class == domain.DataMobile {
		className = "dados móveis"
	}
	fmt.Fprintf(w, "\n  Dados usados neste teste: %s · hoje: %s de %s (%s)\n",
		formatMB(r.BytesUsed()), formatMB(o.usedToday), formatMB(o.dailyLimit), className)

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

func formatMbps(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1) + " Mbps"
}

func formatMB(bytes int64) string {
	return strings.Replace(strconv.FormatFloat(float64(bytes)/domain.MB, 'f', 1, 64), ".", ",", 1) + " MB"
}
