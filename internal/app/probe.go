package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/netinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/wifi"
)

// probeSources are the measurements `avanet probe` combines, injectable so
// the command can be tested without a network.
type probeSources struct {
	network func() (domain.Network, error)
	wifi    func() (domain.WiFi, error)
	latency func(ctx context.Context, gateway netip.Addr, targets []probe.Target) (probe.Round, error)
}

type probeReport struct {
	network    domain.Network
	networkErr error
	wifi       domain.WiFi
	wifiErr    error // nil and wifi unset when the connection is not Wi-Fi
	isWiFi     bool
	round      probe.Round
}

func runProbe(ctx context.Context, args []string, env Env) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	count := flags.Int("n", probe.DefaultEchoOptions.Count, "pacotes por alvo")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%w: probe takes no arguments", errUsage)
	}

	opts := probe.DefaultEchoOptions
	opts.Count = *count
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}

	// Problems that do not stop the diagnosis are shown as warnings.
	logger := slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sources := probeSources{
		network: netinfo.Reader{Logger: logger}.Current,
		wifi:    wifi.Current,
		latency: probe.Latency{Echoer: probe.ICMPEchoer{}, Options: opts}.Run,
	}

	roundTime := time.Duration(opts.Count)*opts.Interval + opts.Wait
	fmt.Fprintf(env.Stdout, "Medindo a conexão (até %d s)...\n\n", int(roundTime.Round(time.Second).Seconds()))

	report, err := collectProbe(ctx, sources)
	if err != nil {
		return err // Main treats an interruption (Ctrl+C) as a normal exit
	}
	renderProbe(env.Stdout, &report, opts.Count)
	return nil
}

func collectProbe(ctx context.Context, src probeSources) (probeReport, error) {
	var r probeReport
	r.network, r.networkErr = src.network()
	if r.networkErr == nil && r.network.Kind == domain.ConnectionWiFi {
		r.isWiFi = true
		r.wifi, r.wifiErr = src.wifi()
	}

	round, err := src.latency(ctx, r.network.Gateway, probe.InternetTargets())
	if err != nil {
		return probeReport{}, fmt.Errorf("measure latency: %w", err)
	}
	r.round = round
	return r, nil
}

func renderProbe(w io.Writer, r *probeReport, count int) {
	fmt.Fprintln(w, "Conexão")
	renderNetwork(w, r)

	if r.isWiFi {
		fmt.Fprintln(w, "\nWi-Fi")
		renderWiFi(w, r)
	}

	fmt.Fprintf(w, "\nLatência (%d pacotes por alvo)\n", count)
	renderLatency(w, r.round)
}

func renderNetwork(w io.Writer, r *probeReport) {
	switch {
	case errors.Is(r.networkErr, netinfo.ErrNoConnection):
		fmt.Fprintln(w, "  Nenhuma interface de rede leva à internet agora.")
		return
	case errors.Is(r.networkErr, netinfo.ErrUnsupported):
		fmt.Fprintln(w, "  Informações de rede ainda não suportadas neste sistema.")
		return
	case r.networkErr != nil:
		fmt.Fprintf(w, "  Não foi possível ler a rede: %v\n", r.networkErr)
		return
	}

	n := &r.network
	tw := newTable(w)
	fmt.Fprintf(tw, "  Tipo\t%s (%s)\n", connectionKindName(n.Kind), n.Description)
	if n.Gateway.IsValid() {
		fmt.Fprintf(tw, "  Roteador\t%s\n", n.Gateway)
	} else {
		fmt.Fprintf(tw, "  Roteador\tnão identificado\n")
	}
	fmt.Fprintf(tw, "  Link\t%d Mbps (velocidade até o roteador, não da internet)\n", n.LinkSpeedMbps)
	fmt.Fprintf(tw, "  Cobrança\t%s\n", costName(n.Cost))
	flush(tw)
}

func renderWiFi(w io.Writer, r *probeReport) {
	switch {
	case errors.Is(r.wifiErr, wifi.ErrAccessDenied):
		fmt.Fprintln(w, "  O Windows bloqueou a leitura do Wi-Fi. Permita que apps da área de trabalho")
		fmt.Fprintln(w, "  usem a localização em Configurações > Privacidade e segurança > Localização.")
		return
	case r.wifiErr != nil:
		fmt.Fprintf(w, "  Não foi possível ler o Wi-Fi: %v\n", r.wifiErr)
		return
	}

	info := &r.wifi
	tw := newTable(w)
	ssid := info.SSID
	if ssid == "" {
		ssid = "(nome não disponível)"
	}
	fmt.Fprintf(tw, "  Rede\t%s\n", ssid)
	signal := fmt.Sprintf("%d%%", info.SignalPercent)
	if info.RSSIdBm != 0 {
		signal += fmt.Sprintf(" (%d dBm)", info.RSSIdBm)
	}
	fmt.Fprintf(tw, "  Sinal\t%s\n", signal)
	fmt.Fprintf(tw, "  Faixa\t%s\n", bandAndChannel(info))
	if info.Standard != "" {
		fmt.Fprintf(tw, "  Padrão\t%s\n", info.Standard)
	}
	fmt.Fprintf(tw, "  Taxa do link\t%d Mbps\n", info.RxRateMbps)
	flush(tw)
}

func renderLatency(w io.Writer, round probe.Round) {
	type row struct {
		name string
		res  probe.TargetResult
	}
	rows := make([]row, 0, len(round.Internet)+1)
	if round.Gateway != nil {
		rows = append(rows, row{"Roteador", *round.Gateway})
	}
	for _, res := range round.Internet {
		rows = append(rows, row{res.Target.Name, res})
	}

	// Numbers align right; padding the names to a common width keeps them
	// aligned left inside the right-aligned table.
	width := 0
	for _, r := range rows {
		width = max(width, len(r.name))
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "  %-*s\tmediana\tjitter\tperda\t\n", width, "")
	for _, r := range rows {
		latencyRow(tw, fmt.Sprintf("%-*s", width, r.name), r.res)
	}
	flush(tw)

	if best, ok := round.Best(); ok {
		fmt.Fprintf(w, "\n  Internet: %s (melhor alvo: %s)\n", ptbr.Millis(best.Stats.Median), best.Target.Name)
	} else {
		fmt.Fprintln(w, "\n  Internet: nenhum alvo respondeu. Sem conexão com a internet?")
	}
}

func latencyRow(w io.Writer, name string, res probe.TargetResult) {
	switch {
	case res.Err != nil:
		fmt.Fprintf(w, "  %s\terro\t\t\t%v\n", name, res.Err)
	case !res.Stats.Reachable():
		fmt.Fprintf(w, "  %s\t—\t—\t%s\t\n", name, ptbr.Percent(res.Stats.LossPercent()))
	default:
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\n", name,
			ptbr.Millis(res.Stats.Median), ptbr.Millis(res.Stats.Jitter), ptbr.Percent(res.Stats.LossPercent()))
	}
}

func newTable(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0) }

// flush ignores the error on purpose: like the fmt.Fprint calls feeding it,
// it only fails if the terminal itself is gone.
func flush(tw *tabwriter.Writer) { _ = tw.Flush() }

func connectionKindName(k domain.ConnectionKind) string {
	switch k {
	case domain.ConnectionEthernet:
		return "Cabo"
	case domain.ConnectionWiFi:
		return "Wi-Fi"
	case domain.ConnectionMobile:
		return "Modem móvel"
	case domain.ConnectionOther:
		return "Outro"
	}
	return "Desconhecido"
}

func costName(c domain.ConnectionCost) string {
	switch c {
	case domain.CostMetered:
		return "limitada (o Windows trata como dados móveis/franquia)"
	case domain.CostUnmetered:
		return "ilimitada"
	case domain.CostUnknown:
		return "desconhecida"
	}
	return "desconhecida"
}

func bandAndChannel(info *domain.WiFi) string {
	band := "desconhecida"
	if info.Band != domain.BandUnknown {
		band = strings.Replace(info.Band.String(), ".", ",", 1) // 2,4 GHz
	}
	if info.Channel > 0 {
		return fmt.Sprintf("%s · canal %d", band, info.Channel)
	}
	return band
}
