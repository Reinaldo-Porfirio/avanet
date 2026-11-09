package analysis

import (
	"math"
	"slices"
	"time"
)

type timedValue struct {
	at    time.Time
	value float64
}

// newStat computes the summary of a metric; empty input gives a zero Stat.
func newStat(values []timedValue) Stat {
	if len(values) == 0 {
		return Stat{}
	}
	sorted := make([]float64, len(values))
	lowest, highest := values[0], values[0]
	for i, v := range values {
		sorted[i] = v.value
		if v.value < lowest.value {
			lowest = v
		}
		if v.value > highest.value {
			highest = v
		}
	}
	slices.Sort(sorted)
	return Stat{
		Count:  len(values),
		Median: quantile(sorted, 0.5),
		P10:    quantile(sorted, 0.1),
		P90:    quantile(sorted, 0.9),
		Min:    lowest.value,
		MinAt:  lowest.at,
		Max:    highest.value,
		MaxAt:  highest.at,
	}
}

// median of unsorted values; 0 when there are none.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return quantile(sorted, 0.5)
}

// quantile interpolates linearly between the two closest ranks of a sorted,
// non-empty slice.
func quantile(sorted []float64, q float64) float64 {
	position := q * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}
