package domain

import (
	"errors"
	"fmt"
	"time"
)

// MB is a decimal megabyte, the unit shown to users ("50 MB").
const MB = 1_000_000

var (
	// ErrMobileDataDisabled means the network is mobile data and the user has
	// not allowed speed tests on it.
	ErrMobileDataDisabled = errors.New("speed tests on mobile data are disabled")
	// ErrBudgetExhausted means today's data budget cannot cover another test.
	ErrBudgetExhausted = errors.New("daily data budget exhausted")
)

// SpeedResult is the outcome of one download/upload test.
type SpeedResult struct {
	Start        time.Time
	Server       string        // e.g. "Cloudflare GRU" (the data center, not the user)
	IdleLatency  time.Duration // round trip to the server before loading the link
	DownloadMbps float64
	UploadMbps   float64
	BytesDown    int64 // data actually used, counted on the wire payload
	BytesUp      int64
	// DownloadCapped/UploadCapped: the test hit its byte cap before a transfer
	// lasted long enough, so a very fast link may be underestimated.
	DownloadCapped bool
	UploadCapped   bool
	// LocalTraffic: other programs used the network during the test, so the
	// result may be lower than what the connection can deliver.
	LocalTraffic bool
}

// BytesUsed is the total data the test consumed.
func (r SpeedResult) BytesUsed() int64 { return r.BytesDown + r.BytesUp }

// DataClass groups networks by who pays for each byte.
type DataClass int

// Data classes.
const (
	DataFixed  DataClass = iota // home broadband: generous daily budget
	DataMobile                  // modem or hotspot: small budget, off by default
)

func (c DataClass) String() string {
	switch c {
	case DataFixed:
		return "fixed"
	case DataMobile:
		return "mobile"
	}
	return "unknown"
}

// ClassOf returns the data class whose budget applies to a network.
func ClassOf(n Network) DataClass {
	if n.IsMobileData() {
		return DataMobile
	}
	return DataFixed
}

// TransferLimits caps the bytes one speed test may use in each direction.
type TransferLimits struct {
	DownloadBytes int64
	UploadBytes   int64
}

// Total is the most a test with these limits can consume.
func (l TransferLimits) Total() int64 { return l.DownloadBytes + l.UploadBytes }

// ClassBudget is the data allowance of one data class.
type ClassBudget struct {
	DailyBytes int64
	PerTest    TransferLimits
}

// DataBudget is the user's data allowance for speed tests.
type DataBudget struct {
	Fixed         ClassBudget
	Mobile        ClassBudget
	MobileEnabled bool
}

// MaxMobileDailyBytes is the highest mobile allowance a user may configure.
const MaxMobileDailyBytes = 200 * MB

// DefaultDataBudget returns the defaults from the plan (section 6). Per-test
// limits fit the transfer ladder: 100 MB down / 45 MB up on broadband reaches
// the 50 MB step; 6 MB / 2 MB on mobile data stops at 4 MB and 1 MB.
func DefaultDataBudget() DataBudget {
	return DataBudget{
		Fixed:  ClassBudget{DailyBytes: 2000 * MB, PerTest: TransferLimits{DownloadBytes: 100 * MB, UploadBytes: 45 * MB}},
		Mobile: ClassBudget{DailyBytes: 50 * MB, PerTest: TransferLimits{DownloadBytes: 6 * MB, UploadBytes: 2 * MB}},
	}
}

// Validate rejects budgets that cannot work or that exceed the mobile ceiling.
func (b DataBudget) Validate() error {
	for _, c := range []struct {
		name   string
		budget ClassBudget
	}{{"fixed", b.Fixed}, {"mobile", b.Mobile}} {
		per := c.budget.PerTest
		if per.DownloadBytes <= 0 || per.UploadBytes <= 0 {
			return fmt.Errorf("%w: %s per-test limits must be positive", ErrInvalidSample, c.name)
		}
		if per.Total() > c.budget.DailyBytes {
			return fmt.Errorf("%w: %s daily budget is smaller than one test", ErrInvalidSample, c.name)
		}
	}
	if b.Mobile.DailyBytes > MaxMobileDailyBytes {
		return fmt.Errorf("%w: mobile daily budget above %d MB", ErrInvalidSample, MaxMobileDailyBytes/MB)
	}
	return nil
}

// Plan returns the limits for the next test on a network of this class, given
// the bytes already used today, or why no test may run. A test only starts if
// the remaining budget covers its worst case, so the budget is never exceeded.
func (b DataBudget) Plan(class DataClass, usedToday int64) (TransferLimits, error) {
	budget := b.Fixed
	if class == DataMobile {
		if !b.MobileEnabled {
			return TransferLimits{}, ErrMobileDataDisabled
		}
		budget = b.Mobile
	}

	if remaining := budget.DailyBytes - usedToday; remaining < budget.PerTest.Total() {
		return TransferLimits{}, fmt.Errorf("%w: %d of %d MB used today on %s data",
			ErrBudgetExhausted, usedToday/MB, budget.DailyBytes/MB, class)
	}
	return budget.PerTest, nil
}

// Daily returns the daily allowance of a class.
func (b DataBudget) Daily(class DataClass) int64 {
	if class == DataMobile {
		return b.Mobile.DailyBytes
	}
	return b.Fixed.DailyBytes
}

// InterfaceCounters are the cumulative bytes a network interface has moved.
type InterfaceCounters struct {
	RxBytes uint64
	TxBytes uint64
}

// LocalTrafficSuspected compares what the interface moved during a test with
// what the test itself transferred. Interface counters also include TCP/IP and
// TLS overhead (a few percent), hence the margin: the excess must exceed both
// 15% of the test and 1 MB.
func LocalTrafficSuspected(before, after InterfaceCounters, testBytes int64) bool {
	if after.RxBytes < before.RxBytes || after.TxBytes < before.TxBytes {
		return false // counters reset (adapter restarted): no conclusion possible
	}
	moved := int64(after.RxBytes-before.RxBytes) + int64(after.TxBytes-before.TxBytes) //nolint:gosec // per-test deltas are far below 2^63
	excess := moved - testBytes
	return excess > MB && excess > testBytes*15/100
}

// Day is a calendar day in the user's local time, formatted YYYY-MM-DD.
// Daily budgets and reports follow the user's day, not UTC's.
type Day string

// DayOf returns the calendar day of t in t's location.
func DayOf(t time.Time) Day { return Day(t.Format(time.DateOnly)) }

// Valid reports whether d is a well-formed calendar day.
func (d Day) Valid() bool {
	_, err := time.Parse(time.DateOnly, string(d))
	return err == nil
}
