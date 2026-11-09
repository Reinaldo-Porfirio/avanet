package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/notify"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/report"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

func runReport(ctx context.Context, args []string, env Env) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sendNotification := flags.Bool("notificar", false, "mostra a notificação do Windows")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("%w: report takes at most one day", errUsage)
	}

	now := time.Now()
	day, err := parseDayArg(flags.Arg(0), now)
	if err != nil {
		return err
	}

	return withDB(ctx, env, func(db *storage.DB) error {
		logger := slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		service := &report.Service{
			Store: db, Logger: logger,
			Profile: func(ctx context.Context) (domain.Profile, error) { return loadProfile(ctx, db, logger) },
		}
		daily, err := service.Load(ctx, day, now)
		if errors.Is(err, analysis.ErrNoData) {
			fmt.Fprintf(env.Stderr, "Não há medições em %s.\n", day)
			return errReported
		}
		if err != nil {
			return err
		}
		renderReport(env.Stdout, daily)

		if *sendNotification {
			title, body := report.NotificationText(daily, now)
			if err := setupNotifications(ctx); err != nil {
				return err
			}
			return notify.Toast{}.Notify(title, body)
		}
		return nil
	})
}

// parseDayArg understands "", "ontem", "hoje" and YYYY-MM-DD.
func parseDayArg(arg string, now time.Time) (domain.Day, error) {
	switch strings.ToLower(arg) {
	case "", "ontem":
		return domain.DayOf(now.AddDate(0, 0, -1)), nil
	case "hoje":
		return domain.DayOf(now), nil
	}
	if day := domain.Day(arg); day.Valid() {
		return day, nil
	}
	return "", fmt.Errorf("%w: unknown day %q (use ontem, hoje or YYYY-MM-DD)", errUsage, arg)
}

func renderReport(w io.Writer, d *analysis.Daily) {
	renderReportHeader(w, d)
	renderHourStrip(w, d)
	renderPeriods(w, d)
	renderNumbers(w, d)

	fmt.Fprintf(w, "\nOnde estava o problema?\n  %s\n  %s\n", d.Diagnosis.Title, d.Diagnosis.Detail)
	fmt.Fprintf(w, "\nDados usados em testes de velocidade: %s\n", ptbr.MB(d.DataUsedBytes))
}

func renderReportHeader(w io.Writer, d *analysis.Daily) {
	title := "Boletim de " + d.Day
	if parsed, err := time.ParseInLocation(time.DateOnly, d.Day, time.Local); err == nil {
		title = "Boletim de " + ptbr.DateLong(parsed)
	}
	if d.InProgress {
		title = "Hoje até agora"
	}
	title += " · perfil " + d.ProfileName
	if d.Network != "" {
		title += " · " + d.Network
	}
	fmt.Fprintf(w, "%s\n\n  %s\n  %s\n\n", title, d.Headline, d.Lede)
	fmt.Fprintf(w, "  Nota do dia: %s\n", ptbr.QualityWord(d.Grade))

	monitored := time.Duration(d.Coverage.MonitoredMs) * time.Millisecond
	window := time.Duration(d.Coverage.WindowMs) * time.Millisecond
	fmt.Fprintf(w, "  Monitorado %s de %s · %s · %s\n", ptbr.Hours(monitored), ptbr.Hours(window),
		ptbr.Count(d.Coverage.Rounds, "medição", "medições"),
		ptbr.Count(d.Coverage.SpeedTests, "teste de velocidade", "testes de velocidade"))
	if d.IgnoredSpeedTests > 0 {
		fmt.Fprintf(w, "  %d teste(s) ignorado(s) porque outros programas usavam a rede.\n", d.IgnoredSpeedTests)
	}
}

// renderHourStrip draws the day as one line, one character per hour.
func renderHourStrip(w io.Writer, d *analysis.Daily) {
	var strip strings.Builder
	for i := range d.Hours {
		strip.WriteRune(hourSymbol(d.Hours[i].Grade))
	}
	fmt.Fprintf(w, "\nHora a hora (da 0h às 24h)\n  %s\n  █ boa   ▒ regular   ! ruim   · sem leitura\n", strip.String())
}

func hourSymbol(q domain.Quality) rune {
	switch q {
	case domain.QualityGood:
		return '█'
	case domain.QualityFair:
		return '▒'
	case domain.QualityPoor:
		return '!'
	case domain.QualityUnknown:
		return '·'
	}
	return '·'
}

func renderPeriods(w io.Writer, d *analysis.Daily) {
	fmt.Fprintln(w, "\nPartes do dia")
	tw := newTable(w)
	for i := range d.Periods {
		p := &d.Periods[i]
		detail := ""
		if p.Grade != domain.QualityUnknown {
			detail = "ping " + ptbr.Decimal(p.LatencyMs, 0) + " ms"
			if p.DownloadMbps > 0 {
				detail += " · " + ptbr.Decimal(p.DownloadMbps, 0) + " ↓ / " + ptbr.Decimal(p.UploadMbps, 0) + " ↑ Mbps"
			}
			if p.Outages > 0 {
				detail += fmt.Sprintf(" · %d queda(s)", p.Outages)
			}
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", p.Name, p.Condition, detail)
	}
	flush(tw)
}

func renderNumbers(w io.Writer, d *analysis.Daily) {
	fmt.Fprintln(w, "\nOs números do dia")
	tw := newTable(w)
	statRow(tw, "Download", "Mbps", &d.Download)
	statRow(tw, "Upload", "Mbps", &d.Upload)
	statRow(tw, "Ping", "ms", &d.Latency)
	statRow(tw, "Jitter", "ms", &d.Jitter)
	fmt.Fprintf(tw, "  Perda de pacotes\t%s%%\n", ptbr.Decimal(d.LossPercent, 1))
	fmt.Fprintf(tw, "  Quedas\t%s\n", outagesText(d))
	flush(tw)
}

func statRow(w io.Writer, name, unit string, s *analysis.Stat) {
	if s.Count == 0 {
		fmt.Fprintf(w, "  %s\tsem medições\n", name)
		return
	}
	fmt.Fprintf(w, "  %s\t%s %s\t80%% entre %s e %s\tmín. %s às %s\tmáx. %s às %s\n", name,
		ptbr.Decimal(s.Median, 1), unit, ptbr.Decimal(s.P10, 1), ptbr.Decimal(s.P90, 1),
		ptbr.Decimal(s.Min, 1), clock(s.MinAt), ptbr.Decimal(s.Max, 1), clock(s.MaxAt))
}

func outagesText(d *analysis.Daily) string {
	if len(d.Outages) == 0 {
		return "nenhuma"
	}
	times := make([]string, 0, len(d.Outages))
	for _, o := range d.Outages {
		times = append(times, clock(o.Start))
	}
	return fmt.Sprintf("%d (%s no total) às %s", len(d.Outages),
		ptbr.Hours(time.Duration(d.DowntimeMs)*time.Millisecond), strings.Join(times, ", "))
}

func clock(t time.Time) string { return t.In(time.Local).Format("15:04") }
