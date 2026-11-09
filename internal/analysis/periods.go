package analysis

import (
	"slices"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// partsOfDay are the report's four periods.
var partsOfDay = [periods]struct {
	name     string
	from, to int
}{
	{"Madrugada", 0, 6},
	{"Manhã", 6, 12},
	{"Tarde", 12, 18},
	{"Noite", 18, 24},
}

// A stretch is poor when at least this share of its monitored hours is poor,
// and only good when at most this share is merely fair (and none is poor).
const (
	poorShare = 0.25
	fairShare = 0.25
)

// rollup grades a stretch from the grades of its hours. One bad hour should
// not condemn a whole day, but it must keep it from being called good.
func rollup(hours []Hour) domain.Quality {
	var known, fair, poor int
	for i := range hours {
		switch hours[i].Grade {
		case domain.QualityGood:
			known++
		case domain.QualityFair:
			known++
			fair++
		case domain.QualityPoor:
			known++
			poor++
		case domain.QualityUnknown:
		}
	}
	switch {
	case known == 0:
		return domain.QualityUnknown
	case float64(poor)/float64(known) >= poorShare:
		return domain.QualityPoor
	case poor > 0 || float64(fair)/float64(known) > fairShare:
		return domain.QualityFair
	default:
		return domain.QualityGood
	}
}

func conditionWord(q domain.Quality) string {
	switch q {
	case domain.QualityGood:
		return "Céu limpo"
	case domain.QualityFair:
		return "Nublado"
	case domain.QualityPoor:
		return "Tempestade"
	case domain.QualityUnknown:
		return "Sem leitura"
	}
	return "Sem leitura"
}

func buildPeriods(buckets *[hoursInDay]bucket, hours *[hoursInDay]Hour, outages []OutageSpan, loc *time.Location) [periods]Period {
	var result [periods]Period
	for i, part := range partsOfDay {
		merged := mergeHours(buckets, part.from, part.to)
		m := merged.measured()
		grade := rollup(hours[part.from:part.to])
		p := Period{
			Name: part.name, FromHour: part.from, ToHour: part.to,
			Grade: grade, Condition: conditionWord(grade),
			LatencyMs: millis(m.Latency), DownloadMbps: m.DownloadMbps, UploadMbps: m.UploadMbps,
		}
		for _, o := range outages {
			if h := o.Start.In(loc).Hour(); h >= part.from && h < part.to {
				p.Outages++
			}
		}
		result[i] = p
	}
	return result
}

func mergeHours(buckets *[hoursInDay]bucket, from, to int) bucket {
	var merged bucket
	for h := from; h < to; h++ {
		merged.merge(&buckets[h])
	}
	return merged
}

// hourRanges returns the longest good stretch and the worst stretches: the
// consecutive hours with the day's lowest grade, longest first (at most 3).
// Unmonitored hours break a stretch.
func hourRanges(hours *[hoursInDay]Hour) (best, worst []HourRange) {
	if r := rangesOf(hours, domain.QualityGood); len(r) > 0 {
		best = r[:1]
	}
	worst = rangesOf(hours, domain.QualityPoor)
	if len(worst) == 0 {
		worst = rangesOf(hours, domain.QualityFair)
	}
	if len(worst) > 3 {
		worst = worst[:3]
	}
	return best, worst
}

// rangesOf lists the stretches with a given grade, longest (then earliest) first.
func rangesOf(hours *[hoursInDay]Hour, grade domain.Quality) []HourRange {
	var ranges []HourRange
	for h := 0; h < hoursInDay; h++ {
		if hours[h].Grade != grade {
			continue
		}
		end := h + 1
		for end < hoursInDay && hours[end].Grade == grade {
			end++
		}
		ranges = append(ranges, HourRange{From: h, To: end})
		h = end
	}
	slices.SortStableFunc(ranges, func(a, b HourRange) int { return (b.To - b.From) - (a.To - a.From) })
	return ranges
}
