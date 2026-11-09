package domain

import "fmt"

// Limits of the values a user may choose in the panel.
const (
	MinFixedDailyMB  int64 = 200 // one full broadband test must fit
	MaxFixedDailyMB  int64 = 20_000
	MinMobileDailyMB int64 = 10 // one full mobile test must fit
	MaxMobileDailyMB       = MaxMobileDailyBytes / MB
	MaxPlanMbps            = 100_000.0
)

// Preferences are the choices the user makes in the panel.
type Preferences struct {
	Profile       ProfileID `json:"profile"`
	FixedDailyMB  int64     `json:"fixed_daily_mb"`
	MobileEnabled bool      `json:"mobile_enabled"`
	MobileDailyMB int64     `json:"mobile_daily_mb"`
	// The contracted plan; 0 means the user did not inform it.
	PlanDownloadMbps float64 `json:"plan_download_mbps"`
	PlanUploadMbps   float64 `json:"plan_upload_mbps"`
}

// DefaultPreferences are used until the user chooses otherwise.
func DefaultPreferences() Preferences {
	budget := DefaultDataBudget()
	return Preferences{
		Profile:       DefaultProfile,
		FixedDailyMB:  budget.Fixed.DailyBytes / MB,
		MobileEnabled: budget.MobileEnabled,
		MobileDailyMB: budget.Mobile.DailyBytes / MB,
	}
}

// Validate rejects values outside what the panel offers. The API must not
// trust the page: anything that writes preferences goes through here.
func (p Preferences) Validate() error {
	if _, ok := ProfileByID(p.Profile); !ok {
		return fmt.Errorf("%w: unknown profile %q", ErrInvalidSample, p.Profile)
	}
	if p.FixedDailyMB < MinFixedDailyMB || p.FixedDailyMB > MaxFixedDailyMB {
		return fmt.Errorf("%w: fixed daily budget of %d MB", ErrInvalidSample, p.FixedDailyMB)
	}
	if p.MobileDailyMB < MinMobileDailyMB || p.MobileDailyMB > MaxMobileDailyMB {
		return fmt.Errorf("%w: mobile daily budget of %d MB", ErrInvalidSample, p.MobileDailyMB)
	}
	for _, speed := range []float64{p.PlanDownloadMbps, p.PlanUploadMbps} {
		// Written this way so NaN, which fails every comparison, is rejected.
		if !(speed >= 0 && speed <= MaxPlanMbps) {
			return fmt.Errorf("%w: plan speed %v", ErrInvalidSample, speed)
		}
	}
	return nil
}

// Budget turns the preferences into the data budget the agent follows.
func (p Preferences) Budget() DataBudget {
	budget := DefaultDataBudget()
	budget.Fixed.DailyBytes = p.FixedDailyMB * MB
	budget.Mobile.DailyBytes = p.MobileDailyMB * MB
	budget.MobileEnabled = p.MobileEnabled
	return budget
}
