package analysis

import (
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// round is one latency round: the router and the best internet target.
type round struct {
	at         time.Time
	hasGateway bool
	gateway    domain.LatencyStats
	reachable  bool
	best       domain.LatencyStats // valid when reachable
	sent, lost int                 // packets counted for the internet loss
}

// groupRounds joins the per-target records of each round. The internet
// measure is the fastest target that answered (see the plan, section 3): the
// connection is at least that good.
func groupRounds(records []domain.LatencyRecord) []round {
	var rounds []round
	for _, rec := range records {
		if len(rounds) == 0 || !rounds[len(rounds)-1].at.Equal(rec.At) {
			rounds = append(rounds, round{at: rec.At})
		}
		r := &rounds[len(rounds)-1]
		if rec.Target == domain.GatewayTarget {
			r.hasGateway, r.gateway = true, rec.Stats
			continue
		}
		if rec.Stats.Reachable() && (!r.reachable || rec.Stats.Median < r.best.Median) {
			r.reachable, r.best = true, rec.Stats
		}
		if !r.reachable {
			r.sent, r.lost = rec.Stats.Sent, rec.Stats.Sent // all lost so far
		}
	}
	for i := range rounds {
		if r := &rounds[i]; r.reachable {
			r.sent, r.lost = r.best.Sent, r.best.Sent-r.best.Received
		}
	}
	return rounds
}

// bucket accumulates raw values for a stretch of time.
type bucket struct {
	rounds              int
	latency, jitter     []float64
	sent, lost          int
	gateway             []float64
	gatewaySent, gwLost int
	download, upload    []float64
	signal              []float64
	downtime            time.Duration
}

func (b *bucket) addRound(r *round) {
	b.rounds++
	b.sent += r.sent
	b.lost += r.lost
	if r.reachable {
		b.latency = append(b.latency, millis(r.best.Median))
		b.jitter = append(b.jitter, millis(r.best.Jitter))
	}
	if r.hasGateway {
		b.gatewaySent += r.gateway.Sent
		b.gwLost += r.gateway.Sent - r.gateway.Received
		if r.gateway.Reachable() {
			b.gateway = append(b.gateway, millis(r.gateway.Median))
		}
	}
}

func (b *bucket) merge(o *bucket) {
	b.rounds += o.rounds
	b.sent += o.sent
	b.lost += o.lost
	b.gatewaySent += o.gatewaySent
	b.gwLost += o.gwLost
	b.downtime += o.downtime
	b.latency = append(b.latency, o.latency...)
	b.jitter = append(b.jitter, o.jitter...)
	b.gateway = append(b.gateway, o.gateway...)
	b.download = append(b.download, o.download...)
	b.upload = append(b.upload, o.upload...)
	b.signal = append(b.signal, o.signal...)
}

func (b *bucket) lossPercent() float64 { return percent(b.lost, b.sent) }

func (b *bucket) gatewayLossPercent() float64 { return percent(b.gwLost, b.gatewaySent) }

func percent(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}

// measured is the bucket as the profile limits see it. A stretch in which
// nothing answered still counts as measured: its 100% loss is the finding.
func (b *bucket) measured() domain.Measured {
	m := domain.Measured{
		HasLatency:  b.rounds > 0,
		Latency:     fromMillis(median(b.latency)),
		Jitter:      fromMillis(median(b.jitter)),
		LossPercent: b.lossPercent(),
		Downtime:    b.downtime,
	}
	if len(b.download) > 0 {
		m.HasDownload, m.DownloadMbps = true, median(b.download)
	}
	if len(b.upload) > 0 {
		m.HasUpload, m.UploadMbps = true, median(b.upload)
	}
	return m
}

func (b *bucket) summary(limits domain.Limits) Hour {
	m := b.measured()
	return Hour{
		Grade:        limits.Grade(m),
		Rounds:       b.rounds,
		LatencyMs:    millis(m.Latency),
		JitterMs:     millis(m.Jitter),
		LossPercent:  m.LossPercent,
		GatewayMs:    median(b.gateway),
		DownloadMbps: m.DownloadMbps,
		UploadMbps:   m.UploadMbps,
		DowntimeMs:   millis(b.downtime),
	}
}

func fromMillis(ms float64) time.Duration { return time.Duration(ms * float64(time.Millisecond)) }

// fillBuckets distributes the day's measurements into its hours.
func fillBuckets(rounds []round, data *domain.DayData, loc *time.Location) [hoursInDay]bucket {
	var buckets [hoursInDay]bucket
	for i := range rounds {
		buckets[rounds[i].at.In(loc).Hour()].addRound(&rounds[i])
	}
	for i := range data.Speed {
		s := &data.Speed[i]
		if s.LocalTraffic {
			continue // other programs were using the link: not representative
		}
		b := &buckets[s.Start.In(loc).Hour()]
		b.download = append(b.download, s.DownloadMbps)
		b.upload = append(b.upload, s.UploadMbps)
	}
	for _, w := range data.WiFi {
		b := &buckets[w.At.In(loc).Hour()]
		b.signal = append(b.signal, float64(w.SignalPercent))
	}
	return buckets
}

// applyOutages clamps outages to the day, adds the downtime to the hours it
// falls in and returns the spans and their total duration in milliseconds. An
// outage that never closed counts until the end of the observed window.
func applyOutages(buckets *[hoursInDay]bucket, outages []domain.Outage, start, windowEnd time.Time) (spans []OutageSpan, totalMs float64) {
	spans = make([]OutageSpan, 0, len(outages))
	loc := start.Location()
	var total time.Duration
	for _, o := range outages {
		from, to := o.Start, o.End
		if to.IsZero() || to.After(windowEnd) {
			to = windowEnd
		}
		if from.Before(start) {
			from = start
		}
		if !to.After(from) {
			continue
		}
		spans = append(spans, OutageSpan{Start: from, End: to})
		total += to.Sub(from)

		// Walk the outage hour by hour, splitting it at each hour boundary.
		for cursor := from; cursor.Before(to); {
			local := cursor.In(loc)
			next := time.Date(local.Year(), local.Month(), local.Day(), local.Hour()+1, 0, 0, 0, loc)
			if next.After(to) {
				next = to
			}
			buckets[local.Hour()].downtime += next.Sub(cursor)
			cursor = next
		}
	}
	return spans, millis(total)
}

// latencyStats summarizes the rounds of the whole day.
func latencyStats(rounds []round) (latency, jitter Stat, lossPercent float64) {
	var latencies, jitters []timedValue
	var sent, lost int
	for i := range rounds {
		r := &rounds[i]
		sent += r.sent
		lost += r.lost
		if r.reachable {
			latencies = append(latencies, timedValue{r.at, millis(r.best.Median)})
			jitters = append(jitters, timedValue{r.at, millis(r.best.Jitter)})
		}
	}
	return newStat(latencies), newStat(jitters), percent(lost, sent)
}

// speedStats summarizes speed tests, leaving out those disturbed by local traffic.
func speedStats(tests []domain.SpeedResult) (download, upload Stat, points []SpeedPoint, ignored int) {
	var downs, ups []timedValue
	points = make([]SpeedPoint, 0, len(tests))
	for i := range tests {
		s := &tests[i]
		points = append(points, SpeedPoint{At: s.Start, DownloadMbps: s.DownloadMbps, UploadMbps: s.UploadMbps, LocalTraffic: s.LocalTraffic})
		if s.LocalTraffic {
			ignored++
			continue
		}
		downs = append(downs, timedValue{s.Start, s.DownloadMbps})
		ups = append(ups, timedValue{s.Start, s.UploadMbps})
	}
	return newStat(downs), newStat(ups), points, ignored
}
