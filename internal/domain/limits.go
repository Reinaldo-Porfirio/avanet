package domain

import "time"

// Limits say what counts as good or poor for one usage profile (plan,
// section 5). A zero value in a field means that metric does not affect the
// grade for this profile.
type Limits struct {
	GoodLatency, PoorLatency   time.Duration
	GoodJitter                 time.Duration
	GoodLoss, PoorLoss         float64 // percent
	GoodDownload, PoorDownload float64 // Mbps
	GoodUpload, PoorUpload     float64 // Mbps
}

// Measured is what is known about a stretch of time. Has* is false when that
// metric was not measured in it (speed tests are sparse).
type Measured struct {
	HasLatency  bool
	Latency     time.Duration
	Jitter      time.Duration
	LossPercent float64

	HasDownload  bool
	DownloadMbps float64
	HasUpload    bool
	UploadMbps   float64

	// Downtime is how long the internet was out in this stretch.
	Downtime time.Duration
}

// A brief drop caps the grade at fair; this much downtime makes it poor.
const poorDowntime = 5 * time.Minute

// Grade rates measurements against the limits: poor if any metric is past
// its poor limit, good only if every measured metric is within its good
// limit, fair otherwise. With nothing measured the grade is unknown.
func (l Limits) Grade(m Measured) Quality {
	if !m.HasLatency && !m.HasDownload && !m.HasUpload {
		return QualityUnknown
	}
	if l.isPoor(m) {
		return QualityPoor
	}
	if l.isGood(m) {
		return QualityGood
	}
	return QualityFair
}

func (l Limits) isPoor(m Measured) bool {
	switch {
	case m.Downtime >= poorDowntime:
		return true
	case m.HasLatency && over(m.Latency, l.PoorLatency):
		return true
	case m.HasLatency && l.PoorLoss > 0 && m.LossPercent > l.PoorLoss:
		return true
	case m.HasDownload && l.PoorDownload > 0 && m.DownloadMbps < l.PoorDownload:
		return true
	case m.HasUpload && l.PoorUpload > 0 && m.UploadMbps < l.PoorUpload:
		return true
	}
	return false
}

func (l Limits) isGood(m Measured) bool {
	switch {
	case m.Downtime > 0:
		return false
	case m.HasLatency && (over(m.Latency, l.GoodLatency) || over(m.Jitter, l.GoodJitter)):
		return false
	case m.HasLatency && l.GoodLoss > 0 && m.LossPercent >= l.GoodLoss:
		return false
	case m.HasDownload && l.GoodDownload > 0 && m.DownloadMbps < l.GoodDownload:
		return false
	case m.HasUpload && l.GoodUpload > 0 && m.UploadMbps < l.GoodUpload:
		return false
	}
	return true
}

// over reports whether value exceeds a limit that is in use.
func over(value, limit time.Duration) bool { return limit > 0 && value > limit }

// limitsFor holds the starting values from the plan, to be calibrated with
// real data. Latency limits for non-gaming profiles are generous: browsing
// and video tolerate delay that would ruin a game.
func limitsFor(id ProfileID) Limits {
	const ms = time.Millisecond
	switch id {
	case ProfileGaming:
		return Limits{GoodLatency: 50 * ms, PoorLatency: 100 * ms, GoodJitter: 15 * ms, GoodLoss: 1, PoorLoss: 2}
	case ProfileHomeOffice:
		return Limits{GoodLatency: 100 * ms, PoorLatency: 250 * ms, GoodJitter: 30 * ms, GoodLoss: 1, PoorLoss: 3,
			GoodUpload: 3, PoorUpload: 1.5}
	case ProfileStreaming:
		return Limits{PoorLatency: 500 * ms, PoorLoss: 10, GoodDownload: 25, PoorDownload: 5}
	case ProfileComplete:
		return Limits{GoodLatency: 50 * ms, PoorLatency: 100 * ms, GoodJitter: 30 * ms, GoodLoss: 1, PoorLoss: 3,
			GoodDownload: 25, PoorDownload: 5, GoodUpload: 3, PoorUpload: 1.5}
	case ProfileBasic:
		return Limits{PoorLatency: 500 * ms, PoorLoss: 10, GoodDownload: 10, PoorDownload: 2}
	}
	return limitsFor(DefaultProfile)
}
