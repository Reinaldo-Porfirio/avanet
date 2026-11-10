package analysis

import (
	"slices"
	"strconv"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
)

// Windows of days the insights look at.
const (
	PatternWindowDays = 7  // recent enough to describe the current routine
	PlanWindowDays    = 30 // the "monthly average" of the reference rule
)

const (
	// minPatternDays is the fewest days with data that can show a pattern.
	minPatternDays = 3
	// recurringShare of those days must agree for an hour to be recurring.
	// With up to 4 days it means every day; from 5 days on, one may differ.
	recurringShare = 0.8
	// minRoundsPerHour: an hour with fewer measurements says too little.
	minRoundsPerHour = 3
	maxBadPatterns   = 2
	// A speed drop smaller than this is not worth mentioning.
	notableDrop = 0.2
	// Latency at least this many times the usual one is a notable rise.
	notableRise = 1.5
)

// Reference shares of the contracted speed. They come from Anatel's rule for
// fixed broadband (Resolução 574/2011), still quoted by Anatel's consumer
// guide; the current regulation (RQUAL, Resolução 717/2019) moved the
// criteria to separate documents. So the panel calls them a reference, never
// a legal verdict. See the plan, section 7.
const (
	ReferenceAveragePercent = 80
	ReferenceMinimumPercent = 40
)

// minPlanTests and minPlanDays: fewer tests than this give no verdict. Ten is
// also how many tests Anatel's official channel asks for a complaint.
const (
	minPlanTests = 10
	minPlanDays  = 3
)

// Confidence grows with the number of days behind a pattern.
type Confidence string

// Confidence levels.
const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// PatternKind tells a recurring problem from a recurring good stretch.
type PatternKind string

// Pattern kinds.
const (
	PatternBad  PatternKind = "bad"
	PatternGood PatternKind = "good"
)

// Insights look across several days.
type Insights struct {
	Until    domain.Day      `json:"until"` // last day considered
	Patterns PatternReport   `json:"patterns"`
	Plan     *PlanComparison `json:"plan,omitempty"` // nil when no plan was informed
}

// PatternReport lists the recurring stretches of the last days.
type PatternReport struct {
	Days     int       `json:"days"` // days with measurements in the window
	Patterns []Pattern `json:"patterns"`
}

// Pattern is a stretch of hours that behaved the same way day after day.
type Pattern struct {
	Kind       PatternKind `json:"kind"`
	Range      HourRange   `json:"range"`
	Days       int         `json:"days"` // days in which these hours were measured
	Hits       int         `json:"hits"` // days in which the pattern happened
	Confidence Confidence  `json:"confidence"`
	Text       string      `json:"text"`
}

// PlanComparison sets the measured speed against the contracted plan.
type PlanComparison struct {
	PlanDownloadMbps float64     `json:"plan_download_mbps"`
	PlanUploadMbps   float64     `json:"plan_upload_mbps"`
	Days             int         `json:"days"`  // days with valid tests
	Tests            int         `json:"tests"` // tests not disturbed by other programs
	Enough           bool        `json:"enough"`
	Download         *PlanMetric `json:"download,omitempty"` // nil when the plan's download is unknown
	Upload           *PlanMetric `json:"upload,omitempty"`
	// ViaWiFi: most tests ran over Wi-Fi, whose own limits count against the
	// operator. A complaint should rely on a cable.
	ViaWiFi bool `json:"via_wifi"`
	// Capped: some test hit its data cap; a very fast plan may be
	// underestimated.
	Capped bool `json:"capped"`

	ReferenceAveragePercent int `json:"reference_average_percent"`
	ReferenceMinimumPercent int `json:"reference_minimum_percent"`
}

// PlanMetric compares one direction (download or upload) with the plan.
type PlanMetric struct {
	AverageMbps    float64   `json:"average_mbps"`
	AveragePercent float64   `json:"average_percent"`
	MinimumMbps    float64   `json:"minimum_mbps"`
	MinimumPercent float64   `json:"minimum_percent"`
	MinimumAt      time.Time `json:"minimum_at"`
	// BelowMinimum counts the tests under the reference minimum.
	BelowMinimum int  `json:"below_minimum"`
	MeetsAverage bool `json:"meets_average"`
	MeetsMinimum bool `json:"meets_minimum"`
}

// FindPatterns looks for hours that went the same way on most of the given
// days. Days must be finished; order does not matter.
func FindPatterns(days []*Daily) PatternReport {
	report := PatternReport{Patterns: []Pattern{}}
	for _, d := range days {
		if d.Coverage.Rounds > 0 {
			report.Days++
		}
	}

	var hours [hoursInDay]hourHistory
	for h := range hours {
		hours[h] = historyOf(days, h)
	}

	bad := recurring(&hours, func(x *hourHistory) int { return x.bad })
	slices.SortStableFunc(bad, func(a, b HourRange) int { return (b.To - b.From) - (a.To - a.From) })
	for _, r := range bad[:min(len(bad), maxBadPatterns)] {
		report.Patterns = append(report.Patterns, badPattern(days, &hours, r))
	}

	good := recurring(&hours, func(x *hourHistory) int { return x.good })
	slices.SortStableFunc(good, func(a, b HourRange) int { return (b.To - b.From) - (a.To - a.From) })
	if len(good) > 0 && good[0].To-good[0].From >= 2 {
		report.Patterns = append(report.Patterns, goodPattern(&hours, good[0]))
	}
	return report
}

// hourHistory is one hour of the day across several days.
type hourHistory struct {
	measured, bad, good int
}

func historyOf(days []*Daily, h int) hourHistory {
	var x hourHistory
	for _, d := range days {
		hour := &d.Hours[h]
		if hour.Rounds < minRoundsPerHour {
			continue
		}
		x.measured++
		switch hour.Grade {
		case domain.QualityGood:
			x.good++
		case domain.QualityFair, domain.QualityPoor:
			x.bad++
		case domain.QualityUnknown:
		}
	}
	return x
}

// recurring returns the stretches of consecutive hours where count(hour)
// happened on enough of the days that measured them.
func recurring(hours *[hoursInDay]hourHistory, count func(*hourHistory) int) []HourRange {
	matches := func(h int) bool {
		x := &hours[h]
		return x.measured >= minPatternDays && float64(count(x)) >= recurringShare*float64(x.measured)
	}
	var ranges []HourRange
	for h := 0; h < hoursInDay; h++ {
		if !matches(h) {
			continue
		}
		end := h + 1
		for end < hoursInDay && matches(end) {
			end++
		}
		ranges = append(ranges, HourRange{From: h, To: end})
		h = end
	}
	return ranges
}

// strength is the weakest hour of a stretch: what can be said of all of it.
func strength(hours *[hoursInDay]hourHistory, r HourRange, count func(*hourHistory) int) (measured, hits int) {
	measured, hits = hours[r.From].measured, count(&hours[r.From])
	for h := r.From + 1; h < r.To; h++ {
		measured, hits = min(measured, hours[h].measured), min(hits, count(&hours[h]))
	}
	return measured, hits
}

func confidenceFor(days int) Confidence {
	switch {
	case days >= 6:
		return ConfidenceHigh
	case days >= 4:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func lead(hits, days int) string {
	if hits == days {
		return "Nos últimos " + strconv.Itoa(days) + " dias"
	}
	return "Em " + strconv.Itoa(hits) + " dos últimos " + strconv.Itoa(days) + " dias"
}

func goodPattern(hours *[hoursInDay]hourHistory, r HourRange) Pattern {
	measured, hits := strength(hours, r, func(x *hourHistory) int { return x.good })
	return Pattern{
		Kind: PatternGood, Range: r, Days: measured, Hits: hits, Confidence: confidenceFor(measured),
		Text: lead(hits, measured) + ", sua internet ficou estável " + rangeLabel(r) + ".",
	}
}

func badPattern(days []*Daily, hours *[hoursInDay]hourHistory, r HourRange) Pattern {
	measured, hits := strength(hours, r, func(x *hourHistory) int { return x.bad })
	inside, outside := comparison(days, r)
	return Pattern{
		Kind: PatternBad, Range: r, Days: measured, Hits: hits, Confidence: confidenceFor(measured),
		Text: lead(hits, measured) + ", " + change(&inside, &outside) + " " + rangeLabel(r) + ". " + likelyCause(&inside),
	}
}

// typical holds the usual values of a set of hours across days.
type typical struct {
	download, latency, gateway float64
}

// comparison averages the stretch's hours and the good hours elsewhere.
func comparison(days []*Daily, r HourRange) (inside, outside typical) {
	var in, out [3][]float64
	for _, d := range days {
		for h := range d.Hours {
			hour := &d.Hours[h]
			if hour.Rounds < minRoundsPerHour {
				continue
			}
			target := &out
			switch {
			case h >= r.From && h < r.To:
				target = &in
			case hour.Grade != domain.QualityGood:
				continue // the reference is how the connection usually behaves when fine
			}
			if hour.DownloadMbps > 0 {
				target[0] = append(target[0], hour.DownloadMbps)
			}
			target[1] = append(target[1], hour.LatencyMs)
			if hour.GatewayMs > 0 {
				target[2] = append(target[2], hour.GatewayMs)
			}
		}
	}
	return typical{median(in[0]), median(in[1]), median(in[2])}, typical{median(out[0]), median(out[1]), median(out[2])}
}

func change(inside, outside *typical) string {
	if inside.download > 0 && outside.download > 0 {
		if drop := 1 - inside.download/outside.download; drop >= notableDrop {
			return "sua velocidade caiu em média " + ptbr.Decimal(drop*100, 0) + "%"
		}
	}
	if outside.latency > 0 && inside.latency >= notableRise*outside.latency {
		return "seu ping subiu de " + ptbr.Decimal(outside.latency, 0) + " para " + ptbr.Decimal(inside.latency, 0) + " ms"
	}
	return "sua internet ficou abaixo do que o seu perfil precisa"
}

func likelyCause(inside *typical) string {
	switch {
	case inside.gateway > localLatencyLimit:
		return "O atraso já aparece no roteador: costuma ser o Wi-Fi ou muitos aparelhos usando a rede ao mesmo tempo."
	case inside.gateway > 0:
		return "O roteador seguiu respondendo bem, o que costuma indicar congestionamento na operadora no horário de pico."
	default:
		return "Não deu para medir o roteador nesse horário, então a causa não ficou clara."
	}
}

// ComparePlan sets the speed tests of the given days against the contracted
// plan. It returns nil when no plan speed was informed.
func ComparePlan(days []*Daily, planDownloadMbps, planUploadMbps float64) *PlanComparison {
	if planDownloadMbps <= 0 && planUploadMbps <= 0 {
		return nil
	}
	c := &PlanComparison{
		PlanDownloadMbps: planDownloadMbps, PlanUploadMbps: planUploadMbps,
		ReferenceAveragePercent: ReferenceAveragePercent, ReferenceMinimumPercent: ReferenceMinimumPercent,
	}
	var downs, ups []timedValue
	wifiTests := 0
	for _, d := range days {
		dayTests := 0
		for _, p := range d.Speed {
			if p.LocalTraffic {
				continue // other programs shared the link: not the operator's fault
			}
			dayTests++
			downs = append(downs, timedValue{p.At, p.DownloadMbps})
			ups = append(ups, timedValue{p.At, p.UploadMbps})
			c.Capped = c.Capped || p.Capped
			if d.Connection == domain.ConnectionWiFi.String() {
				wifiTests++
			}
		}
		if dayTests > 0 {
			c.Days++
			c.Tests += dayTests
		}
	}
	c.Enough = c.Tests >= minPlanTests && c.Days >= minPlanDays
	c.ViaWiFi = wifiTests*2 > c.Tests
	if planDownloadMbps > 0 && len(downs) > 0 {
		c.Download = planMetric(downs, planDownloadMbps)
	}
	if planUploadMbps > 0 && len(ups) > 0 {
		c.Upload = planMetric(ups, planUploadMbps)
	}
	return c
}

func planMetric(values []timedValue, plan float64) *PlanMetric {
	m := &PlanMetric{MinimumMbps: values[0].value, MinimumAt: values[0].at}
	floor := plan * ReferenceMinimumPercent / 100
	var sum float64
	for _, v := range values {
		sum += v.value
		if v.value < m.MinimumMbps {
			m.MinimumMbps, m.MinimumAt = v.value, v.at
		}
		if v.value < floor {
			m.BelowMinimum++
		}
	}
	m.AverageMbps = sum / float64(len(values))
	m.AveragePercent = 100 * m.AverageMbps / plan
	m.MinimumPercent = 100 * m.MinimumMbps / plan
	m.MeetsAverage = m.AveragePercent >= ReferenceAveragePercent
	m.MeetsMinimum = m.BelowMinimum == 0
	return m
}
