// Package report produces the daily reports: at 8h it builds what is missing,
// stores it and tells the user; on demand it loads any day's report.
package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

const (
	// readyHour is when yesterday's report becomes due, local time.
	readyHour = 8
	// lookbackDays limits how far back missing reports are built after the
	// computer was off for a while.
	lookbackDays = 7
	// retentionDays is how long raw samples are kept. Older days live on in
	// their stored reports.
	retentionDays = 90
	// retryAfter spaces out new attempts when building or notifying fails.
	retryAfter = time.Hour
	// maxListedReports is about two years of daily reports.
	maxListedReports = 730
	// recentListedDays are checked for measurements, because a day that was
	// never turned into a report can still be opened.
	recentListedDays = 35
)

// DayEntry is one line of the list of reports. Grade and Headline are empty
// for a day whose report was not built yet.
type DayEntry struct {
	Day      domain.Day `json:"day"`
	Grade    string     `json:"grade,omitempty"`
	Headline string     `json:"headline,omitempty"`
}

// Store is the persistence reports need (implemented by *storage.DB).
type Store interface {
	LoadDay(ctx context.Context, day domain.Day, from, to time.Time) (domain.DayData, error)
	HasMeasurements(ctx context.Context, from, to time.Time) (bool, error)
	DailyReport(ctx context.Context, day domain.Day) (storage.StoredReport, bool, error)
	SaveDailyReport(ctx context.Context, r *storage.StoredReport) error
	MarkReportNotified(ctx context.Context, day domain.Day, at time.Time) error
	PruneRawData(ctx context.Context, before time.Time) error
	ReportSummaries(ctx context.Context, limit int) ([]storage.ReportSummary, error)
}

// Notifier tells the user a report is ready.
type Notifier interface {
	Notify(title, body string) error
}

// Service builds, stores and announces daily reports. Load and Days can be
// called concurrently with Tick; Tick itself belongs to the agent's goroutine.
type Service struct {
	Store    Store
	Notifier Notifier // may be nil for read-only use
	Logger   *slog.Logger
	// Profile returns the profile currently chosen by the user.
	Profile func(ctx context.Context) (domain.Profile, error)

	doneDay domain.Day // the day whose 8h work finished
	retryAt time.Time
}

// Tick does the day's report work once it is due. It is cheap to call on
// every measurement round, and it also covers a computer that was off at 8h:
// the first round after turning it on finds the report still missing.
func (s *Service) Tick(ctx context.Context, now time.Time) error {
	today := domain.DayOf(now)
	if now.Hour() < readyHour || s.doneDay == today || now.Before(s.retryAt) {
		return nil
	}
	if err := s.catchUp(ctx, now); err != nil {
		s.retryAt = now.Add(retryAfter)
		return err
	}
	if err := s.Store.PruneRawData(ctx, domain.StartOfLocalDay(now).AddDate(0, 0, -retentionDays)); err != nil {
		s.retryAt = now.Add(retryAfter)
		return err
	}
	s.doneDay = today
	return nil
}

// catchUp builds the reports missing from the last days and announces only
// the most recent, so a computer off for a week does not get seven toasts.
func (s *Service) catchUp(ctx context.Context, now time.Time) error {
	midnight := domain.StartOfLocalDay(now)
	var pending []*analysis.Daily
	for offset := lookbackDays; offset >= 1; offset-- {
		from, to := midnight.AddDate(0, 0, -offset), midnight.AddDate(0, 0, -offset+1)
		report, err := s.pendingReport(ctx, from, to, now)
		if err != nil {
			return err
		}
		if report != nil {
			pending = append(pending, report)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	latest := pending[len(pending)-1]
	if s.Notifier != nil {
		title, body := NotificationText(latest, now)
		if err := s.Notifier.Notify(title, body); err != nil {
			return err
		}
		s.Logger.Info("daily report announced", "day", latest.Day)
	}
	for _, r := range pending {
		if err := s.Store.MarkReportNotified(ctx, domain.Day(r.Day), now); err != nil {
			return err
		}
	}
	return nil
}

// pendingReport returns the report of a day the user has not been told
// about, building it if needed; nil when there is nothing to announce.
func (s *Service) pendingReport(ctx context.Context, from, to, now time.Time) (*analysis.Daily, error) {
	day := domain.DayOf(from)
	has, err := s.Store.HasMeasurements(ctx, from, to)
	if err != nil || !has {
		return nil, err
	}
	stored, found, err := s.Store.DailyReport(ctx, day)
	if err != nil {
		return nil, err
	}
	if found && !stored.NotifiedAt.IsZero() {
		return nil, nil
	}
	report, err := s.buildAndSave(ctx, day, from, to, now)
	if errors.Is(err, analysis.ErrNoData) {
		return nil, nil
	}
	return report, err
}

// Load returns the report of any day. Stored reports are reused while they
// match the current profile and format; otherwise they are rebuilt from the
// raw data, which is why changing the profile updates old reports. A day
// still in progress is built but never stored.
func (s *Service) Load(ctx context.Context, day domain.Day, now time.Time) (*analysis.Daily, error) {
	parsed, err := time.ParseInLocation(time.DateOnly, string(day), now.Location())
	if err != nil {
		return nil, fmt.Errorf("%w: day %q", domain.ErrInvalidSample, day)
	}
	from, to := domain.StartOfLocalDay(parsed), domain.EndOfLocalDay(parsed)
	if !now.Before(to) {
		return s.loadFinished(ctx, day, from, to, now)
	}
	if now.Before(from) {
		return nil, analysis.ErrNoData
	}
	return s.build(ctx, day, from, to, now)
}

// Days lists the days that have something to show, newest first.
func (s *Service) Days(ctx context.Context, now time.Time) ([]DayEntry, error) {
	stored, err := s.Store.ReportSummaries(ctx, maxListedReports)
	if err != nil {
		return nil, err
	}
	known := make(map[domain.Day]bool, len(stored))
	entries := make([]DayEntry, 0, len(stored)+recentListedDays)
	for _, r := range stored {
		known[r.Day] = true
		entries = append(entries, DayEntry{Day: r.Day, Grade: r.Grade, Headline: r.Headline})
	}

	midnight := domain.StartOfLocalDay(now)
	for offset := range recentListedDays {
		from := midnight.AddDate(0, 0, -offset)
		day := domain.DayOf(from)
		if known[day] {
			continue
		}
		has, err := s.Store.HasMeasurements(ctx, from, from.AddDate(0, 0, 1))
		if err != nil {
			return nil, err
		}
		if has {
			entries = append(entries, DayEntry{Day: day})
		}
	}
	// Days are YYYY-MM-DD, so sorting the text sorts them by date.
	slices.SortFunc(entries, func(a, b DayEntry) int { return strings.Compare(string(b.Day), string(a.Day)) })
	return entries, nil
}

func (s *Service) loadFinished(ctx context.Context, day domain.Day, from, to, now time.Time) (*analysis.Daily, error) {
	stored, found, err := s.Store.DailyReport(ctx, day)
	if err != nil {
		return nil, err
	}
	if found {
		profile, err := s.Profile(ctx)
		if err != nil {
			return nil, err
		}
		rawGone := from.Before(domain.StartOfLocalDay(now).AddDate(0, 0, -retentionDays))
		current := stored.Version == analysis.Version && stored.Profile == profile.ID
		if current || rawGone {
			return decode(&stored)
		}
	}
	return s.buildAndSave(ctx, day, from, to, now)
}

func decode(stored *storage.StoredReport) (*analysis.Daily, error) {
	var d analysis.Daily
	if err := json.Unmarshal([]byte(stored.JSON), &d); err != nil {
		return nil, fmt.Errorf("decode report of %s: %w", stored.Day, err)
	}
	return &d, nil
}

func (s *Service) buildAndSave(ctx context.Context, day domain.Day, from, to, now time.Time) (*analysis.Daily, error) {
	report, err := s.build(ctx, day, from, to, now)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("encode report of %s: %w", day, err)
	}
	err = s.Store.SaveDailyReport(ctx, &storage.StoredReport{
		Day: day, Profile: domain.ProfileID(report.ProfileID), Version: analysis.Version,
		JSON: string(encoded), GeneratedAt: now,
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func (s *Service) build(ctx context.Context, day domain.Day, from, to, now time.Time) (*analysis.Daily, error) {
	profile, err := s.Profile(ctx)
	if err != nil {
		return nil, err
	}
	data, err := s.Store.LoadDay(ctx, day, from, to)
	if err != nil {
		return nil, err
	}
	return analysis.Build(&analysis.Input{Day: day, Start: from, End: to, Now: now, Profile: profile, Data: &data})
}

// NotificationText writes the notification that announces a report.
func NotificationText(r *analysis.Daily, now time.Time) (title, body string) {
	when := "de ontem"
	if day := domain.DayOf(domain.StartOfLocalDay(now).AddDate(0, 0, -1)); string(day) != r.Day {
		if parsed, err := time.ParseInLocation(time.DateOnly, r.Day, now.Location()); err == nil {
			when = "de " + ptbr.DateShort(parsed)
		}
	}
	return "Seu boletim " + when + " está pronto", r.Headline + " Nota do dia: " + ptbr.QualityWord(r.Grade) + "."
}
