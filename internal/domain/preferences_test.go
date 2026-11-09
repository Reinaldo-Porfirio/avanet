package domain

import (
	"errors"
	"math"
	"testing"
)

func TestDefaultPreferencesAreValidAndKeepMobileOff(t *testing.T) {
	p := DefaultPreferences()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.MobileEnabled {
		t.Error("mobile data must stay off until the user turns it on")
	}
	if err := p.Budget().Validate(); err != nil {
		t.Errorf("default budget: %v", err)
	}
}

func TestPreferencesLimitsProduceValidBudgets(t *testing.T) {
	p := DefaultPreferences()
	p.FixedDailyMB, p.MobileDailyMB = MinFixedDailyMB, MinMobileDailyMB
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := p.Budget().Validate(); err != nil {
		t.Errorf("the smallest allowed values must still fit one test: %v", err)
	}

	p.FixedDailyMB, p.MobileDailyMB = MaxFixedDailyMB, MaxMobileDailyMB
	if err := p.Budget().Validate(); err != nil {
		t.Errorf("the largest allowed values: %v", err)
	}
}

func TestPreferencesValidateRejects(t *testing.T) {
	cases := map[string]func(*Preferences){
		"unknown profile":  func(p *Preferences) { p.Profile = "nope" },
		"fixed too small":  func(p *Preferences) { p.FixedDailyMB = MinFixedDailyMB - 1 },
		"fixed too large":  func(p *Preferences) { p.FixedDailyMB = MaxFixedDailyMB + 1 },
		"mobile too small": func(p *Preferences) { p.MobileDailyMB = MinMobileDailyMB - 1 },
		"mobile too large": func(p *Preferences) { p.MobileDailyMB = MaxMobileDailyMB + 1 },
		"negative plan":    func(p *Preferences) { p.PlanDownloadMbps = -1 },
		"huge plan":        func(p *Preferences) { p.PlanUploadMbps = MaxPlanMbps + 1 },
		"not a number":     func(p *Preferences) { p.PlanUploadMbps = math.NaN() },
		"infinite":         func(p *Preferences) { p.PlanDownloadMbps = math.Inf(1) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := DefaultPreferences()
			change(&p)
			if err := p.Validate(); !errors.Is(err, ErrInvalidSample) {
				t.Errorf("err = %v, want ErrInvalidSample", err)
			}
		})
	}
}

func TestBudgetFollowsPreferences(t *testing.T) {
	p := DefaultPreferences()
	p.FixedDailyMB, p.MobileDailyMB, p.MobileEnabled = 500, 100, true

	b := p.Budget()
	if b.Fixed.DailyBytes != 500*MB || b.Mobile.DailyBytes != 100*MB || !b.MobileEnabled {
		t.Errorf("budget = %+v", b)
	}
	if b.Mobile.PerTest != DefaultDataBudget().Mobile.PerTest {
		t.Error("the per-test limits are not user preferences")
	}
}
