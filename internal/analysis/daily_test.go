package analysis

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

var (
	brt      = time.FixedZone("BRT", -3*3600)
	dayStart = time.Date(2026, 11, 3, 0, 0, 0, 0, brt)
	dayEnd   = dayStart.Add(24 * time.Hour)
)

// at returns a time of the test day.
func at(hour, minute int) time.Time {
	return dayStart.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

// twoPackets summarizes a round of two requests; missing replies were lost.
func twoPackets(t *testing.T, rtts ...time.Duration) domain.LatencyStats {
	t.Helper()
	s, err := domain.SummarizeLatency(rtts, 2)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dayBuilder composes a synthetic day.
type dayBuilder struct {
	t    *testing.T
	data domain.DayData
}

func newDay(t *testing.T) *dayBuilder { t.Helper(); return &dayBuilder{t: t} }

const ms = time.Millisecond

// rounds records one round every 10 minutes during [fromHour, toHour).
func (b *dayBuilder) rounds(fromHour, toHour int, internet, gateway time.Duration) *dayBuilder {
	for h := fromHour; h < toHour; h++ {
		for m := 0; m < 60; m += 10 {
			b.round(at(h, m), internet, gateway)
		}
	}
	b.data.Sessions = append(b.data.Sessions, domain.TimeSpan{Start: at(fromHour, 0), End: at(toHour, 0)})
	return b
}

func (b *dayBuilder) round(when time.Time, internet, gateway time.Duration) {
	b.data.Latency = append(b.data.Latency,
		domain.LatencyRecord{At: when, Target: domain.GatewayTarget, Stats: twoPackets(b.t, gateway, gateway)},
		domain.LatencyRecord{At: when, Target: "Google", Stats: twoPackets(b.t, internet+40*ms, internet+40*ms)},
		domain.LatencyRecord{At: when, Target: "Quad9", Stats: twoPackets(b.t, internet, internet)},
	)
}

func (b *dayBuilder) speed(when time.Time, down, up float64) *dayBuilder {
	b.data.Speed = append(b.data.Speed, domain.SpeedResult{Start: when, DownloadMbps: down, UploadMbps: up})
	return b
}

func (b *dayBuilder) build(id domain.ProfileID) *Daily {
	b.t.Helper()
	p, ok := domain.ProfileByID(id)
	if !ok {
		b.t.Fatalf("unknown profile %q", id)
	}
	d, err := Build(&Input{Day: "2026-11-03", Start: dayStart, End: dayEnd, Now: dayEnd.Add(8 * time.Hour), Profile: p, Data: &b.data})
	if err != nil {
		b.t.Fatal(err)
	}
	return d
}

// conceptDay is the day from the visual concept: a stable day, a bad evening.
func conceptDay(t *testing.T) *dayBuilder {
	t.Helper()
	b := newDay(t).rounds(7, 20, 15*ms, 2*ms).rounds(20, 22, 300*ms, 2*ms)
	for h := 7; h < 20; h++ {
		b.speed(at(h, 5), 280, 40)
	}
	b.speed(at(20, 5), 90, 20).speed(at(21, 5), 40, 9)
	b.data.WiFi = append(b.data.WiFi, domain.WiFiRecord{At: at(21, 0), SignalPercent: 78})
	b.data.Network, b.data.HasNetwork = domain.NetworkRef{Kind: domain.ConnectionWiFi, SSID: "Casa"}, true
	b.data.DataUsed = 1100 * domain.MB
	return b
}

func TestConceptDay(t *testing.T) {
	d := conceptDay(t).build(domain.ProfileHomeOffice)

	if d.Grade != domain.QualityFair {
		t.Errorf("Grade = %v, want fair (2 poor hours in 15)", d.Grade)
	}
	if want := "Dia firme, noite instável."; d.Headline != want {
		t.Errorf("Headline = %q, want %q", d.Headline, want)
	}
	for _, want := range []string{"Das 7h às 20h sua internet ficou estável.", "Das 20h às 22h", "videochamada"} {
		if !strings.Contains(d.Lede, want) {
			t.Errorf("Lede lacks %q: %q", want, d.Lede)
		}
	}
	if d.Network != "Wi-Fi “Casa”" || d.DataUsedBytes != 1100*domain.MB {
		t.Errorf("Network = %q, DataUsedBytes = %d", d.Network, d.DataUsedBytes)
	}

	if d.Coverage.MonitoredMs != millis(15*time.Hour) || d.Coverage.WindowMs != millis(24*time.Hour) {
		t.Errorf("Coverage = %+v, want 15h of 24h", d.Coverage)
	}
	if d.Coverage.Rounds != 15*6 || d.Coverage.SpeedTests != 15 {
		t.Errorf("Coverage counts = %+v", d.Coverage)
	}

	grades := map[int]domain.Quality{0: domain.QualityUnknown, 6: domain.QualityUnknown, 7: domain.QualityGood,
		19: domain.QualityGood, 20: domain.QualityPoor, 21: domain.QualityPoor, 22: domain.QualityUnknown}
	for h, want := range grades {
		if got := d.Hours[h].Grade; got != want {
			t.Errorf("hour %d grade = %v, want %v", h, got, want)
		}
	}

	wantConditions := []string{"Sem leitura", "Céu limpo", "Céu limpo", "Tempestade"}
	for i, want := range wantConditions {
		if d.Periods[i].Condition != want {
			t.Errorf("%s = %q, want %q", d.Periods[i].Name, d.Periods[i].Condition, want)
		}
	}

	if d.Best[0] != (HourRange{7, 20}) || d.Worst[0] != (HourRange{20, 22}) {
		t.Errorf("Best = %v, Worst = %v", d.Best, d.Worst)
	}
	if d.Download.Min != 40 || !d.Download.MinAt.Equal(at(21, 5)) || d.Download.Max != 280 || d.Download.Median != 280 {
		t.Errorf("Download = %+v", d.Download)
	}
	if d.Latency.Median != 15 {
		t.Errorf("Latency median = %v; the best target of each round must be used", d.Latency.Median)
	}

	if d.Diagnosis.Verdict != VerdictProvider || d.Diagnosis.GatewayMs != 2 || d.Diagnosis.InternetMs != 300 {
		t.Errorf("Diagnosis = %+v, want the provider blamed", d.Diagnosis)
	}
	if d.Diagnosis.SignalPercent != 78 {
		t.Errorf("SignalPercent = %v", d.Diagnosis.SignalPercent)
	}
}

func TestGoodDay(t *testing.T) {
	d := newDay(t).rounds(8, 18, 12*ms, 1*ms).speed(at(9, 0), 500, 100).build(domain.ProfileComplete)

	if d.Grade != domain.QualityGood || d.Headline != "Dia firme." {
		t.Errorf("Grade = %v, Headline = %q", d.Grade, d.Headline)
	}
	if d.Lede != "Sua internet ficou estável em todas as horas monitoradas." {
		t.Errorf("Lede = %q", d.Lede)
	}
	if len(d.Worst) != 0 {
		t.Errorf("Worst = %v, want none", d.Worst)
	}
	if d.Diagnosis.Verdict != VerdictNone {
		t.Errorf("Verdict = %v", d.Diagnosis.Verdict)
	}
}

func TestWholeDayBad(t *testing.T) {
	d := newDay(t).rounds(8, 12, 300*ms, 2*ms).build(domain.ProfileGaming)
	if d.Grade != domain.QualityPoor || d.Headline != "Dia difícil." {
		t.Errorf("Grade = %v, Headline = %q", d.Grade, d.Headline)
	}
}

func TestDiagnosisBlamesLocalNetwork(t *testing.T) {
	b := newDay(t).rounds(8, 18, 12*ms, 1*ms).rounds(18, 20, 180*ms, 90*ms)
	b.data.WiFi = append(b.data.WiFi, domain.WiFiRecord{At: at(18, 30), SignalPercent: 22})
	d := b.build(domain.ProfileGaming)

	if d.Diagnosis.Verdict != VerdictLocal || !strings.Contains(d.Diagnosis.Title, "Wi-Fi") {
		t.Errorf("Diagnosis = %+v, want the Wi-Fi blamed", d.Diagnosis)
	}
	if !strings.Contains(d.Diagnosis.Detail, "22%") {
		t.Errorf("Detail lacks the weak signal: %q", d.Diagnosis.Detail)
	}
}

func TestDiagnosisCapacity(t *testing.T) {
	b := newDay(t).rounds(8, 20, 12*ms, 1*ms)
	for h := 8; h < 18; h++ {
		b.speed(at(h, 0), 300, 40)
	}
	b.speed(at(18, 0), 2, 0.5).speed(at(19, 0), 2, 0.5)
	d := b.build(domain.ProfileStreaming)

	if d.Diagnosis.Verdict != VerdictCapacity {
		t.Errorf("Diagnosis = %+v; latency was fine, only speed dropped", d.Diagnosis)
	}
	if !strings.Contains(d.Lede, "menos da metade") {
		t.Errorf("Lede = %q", d.Lede)
	}
}

func TestOutagesAreClampedAndSplitAcrossHours(t *testing.T) {
	b := newDay(t).rounds(0, 24, 12*ms, 1*ms)
	b.data.Outages = []domain.Outage{
		{Start: at(0, 0).Add(-10 * time.Minute), End: at(0, 5)}, // started the day before
		{Start: at(13, 58), End: at(14, 1)},                     // crosses an hour
		{Start: at(23, 50)},                                     // never closed
	}
	d := b.build(domain.ProfileBasic)

	if len(d.Outages) != 3 || !d.Outages[0].Start.Equal(dayStart) {
		t.Fatalf("Outages = %+v", d.Outages)
	}
	if !d.Outages[2].End.Equal(dayEnd) {
		t.Errorf("open outage ends at %v, want the end of the day", d.Outages[2].End)
	}
	if want := millis(18 * time.Minute); d.DowntimeMs != want {
		t.Errorf("DowntimeMs = %v, want %v", d.DowntimeMs, want)
	}
	if d.Hours[13].DowntimeMs != millis(2*time.Minute) || d.Hours[14].DowntimeMs != millis(time.Minute) {
		t.Errorf("split = %v / %v", d.Hours[13].DowntimeMs, d.Hours[14].DowntimeMs)
	}
	if d.Hours[13].Grade != domain.QualityFair {
		t.Errorf("hour with a brief drop = %v, want fair", d.Hours[13].Grade)
	}
	if d.Hours[23].Grade != domain.QualityPoor {
		t.Errorf("hour with 10 min down = %v, want poor", d.Hours[23].Grade)
	}
	if d.Periods[3].Outages != 1 || d.Periods[0].Outages != 1 {
		t.Errorf("outages per period: %d, %d", d.Periods[0].Outages, d.Periods[3].Outages)
	}
}

func TestUnreachableRoundsCountAsLoss(t *testing.T) {
	b := newDay(t).rounds(8, 9, 12*ms, 1*ms)
	// One more round in the same hour, in which nothing answered.
	b.data.Latency = append(b.data.Latency,
		domain.LatencyRecord{At: at(8, 59), Target: domain.GatewayTarget, Stats: twoPackets(t, ms, ms)},
		domain.LatencyRecord{At: at(8, 59), Target: "Quad9", Stats: twoPackets(t)},
	)
	d := b.build(domain.ProfileGaming)

	if want := 100.0 * 2 / 14; math.Abs(d.LossPercent-want) > 1e-9 {
		t.Errorf("LossPercent = %v, want %v", d.LossPercent, want)
	}
	if d.Latency.Count != 6 {
		t.Errorf("unreachable round entered the latency stats: %+v", d.Latency)
	}
}

func TestSpeedTestsWithLocalTrafficAreIgnored(t *testing.T) {
	b := newDay(t).rounds(8, 12, 12*ms, 1*ms).speed(at(9, 0), 300, 40)
	b.data.Speed = append(b.data.Speed, domain.SpeedResult{Start: at(10, 0), DownloadMbps: 3, UploadMbps: 1, LocalTraffic: true})
	d := b.build(domain.ProfileStreaming)

	if d.IgnoredSpeedTests != 1 || d.Download.Count != 1 || d.Download.Min != 300 {
		t.Errorf("ignored = %d, Download = %+v", d.IgnoredSpeedTests, d.Download)
	}
	if d.Hours[10].Grade != domain.QualityGood || len(d.Speed) != 2 {
		t.Errorf("disturbed test must not affect the grade (%v) but stays in the chart (%d)", d.Hours[10].Grade, len(d.Speed))
	}
}

func TestDayInProgress(t *testing.T) {
	b := newDay(t).rounds(8, 10, 12*ms, 1*ms)
	p, _ := domain.ProfileByID(domain.ProfileBasic)
	d, err := Build(&Input{Day: "2026-11-03", Start: dayStart, End: dayEnd, Now: at(10, 0), Profile: p, Data: &b.data})
	if err != nil {
		t.Fatal(err)
	}
	if !d.InProgress || d.Coverage.WindowMs != millis(10*time.Hour) {
		t.Errorf("InProgress = %v, Coverage = %+v", d.InProgress, d.Coverage)
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	p, _ := domain.ProfileByID(domain.ProfileBasic)
	empty := &domain.DayData{}
	if _, err := Build(&Input{Day: "2026-11-03", Start: dayStart, End: dayEnd, Now: dayEnd, Profile: p, Data: empty}); !errors.Is(err, ErrNoData) {
		t.Errorf("empty day: error = %v, want ErrNoData", err)
	}
	if _, err := Build(&Input{Day: "ontem", Start: dayStart, End: dayEnd, Profile: p, Data: empty}); !errors.Is(err, domain.ErrInvalidSample) {
		t.Errorf("bad day: error = %v", err)
	}
	if _, err := Build(&Input{Day: "2026-11-03", Start: dayEnd, End: dayStart, Profile: p, Data: empty}); !errors.Is(err, domain.ErrInvalidSample) {
		t.Errorf("reversed bounds: error = %v", err)
	}
}

func TestReportSurvivesJSON(t *testing.T) {
	d := conceptDay(t).build(domain.ProfileHomeOffice)
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"grade":"fair"`) {
		t.Error("grades must be stored by name")
	}
	var back Daily
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Grade != d.Grade || back.Headline != d.Headline || back.Hours[20].Grade != domain.QualityPoor {
		t.Errorf("round trip changed the report: %+v", back.Grade)
	}
}

func TestQuantile(t *testing.T) {
	values := []float64{10, 20, 30, 40}
	tests := map[float64]float64{0: 10, 1: 40, 0.5: 25, 0.1: 13}
	for q, want := range tests {
		if got := quantile(values, q); math.Abs(got-want) > 1e-9 {
			t.Errorf("quantile(%v) = %v, want %v", q, got, want)
		}
	}
	if median(nil) != 0 {
		t.Error("median of nothing must be 0")
	}
}

func TestRangeLabel(t *testing.T) {
	tests := map[HourRange]string{
		{20, 22}: "das 20h às 22h",
		{13, 14}: "por volta das 13h",
		{0, 6}:   "da meia-noite às 6h",
		{18, 24}: "das 18h à meia-noite",
	}
	for r, want := range tests {
		if got := rangeLabel(r); got != want {
			t.Errorf("rangeLabel(%v) = %q, want %q", r, got, want)
		}
	}
}
