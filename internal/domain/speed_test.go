package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultDataBudgetIsValid(t *testing.T) {
	b := DefaultDataBudget()
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	if b.MobileEnabled {
		t.Error("mobile data must be disabled by default")
	}
}

func TestDataBudgetValidate(t *testing.T) {
	tests := map[string]func(*DataBudget){
		"zero per-test download":      func(b *DataBudget) { b.Fixed.PerTest.DownloadBytes = 0 },
		"test bigger than daily":      func(b *DataBudget) { b.Mobile.DailyBytes = b.Mobile.PerTest.Total() - 1 },
		"mobile above the 200 MB cap": func(b *DataBudget) { b.Mobile.DailyBytes = MaxMobileDailyBytes + 1 },
	}
	for name, mutate := range tests {
		b := DefaultDataBudget()
		mutate(&b)
		if err := b.Validate(); !errors.Is(err, ErrInvalidSample) {
			t.Errorf("%s: error = %v, want ErrInvalidSample", name, err)
		}
	}
}

func TestDataBudgetPlan(t *testing.T) {
	b := DefaultDataBudget()
	fixedTest := b.Fixed.PerTest.Total()

	tests := []struct {
		name          string
		mobileEnabled bool
		class         DataClass
		used          int64
		want          TransferLimits
		wantErr       error
	}{
		{name: "fixed, nothing used", class: DataFixed, want: b.Fixed.PerTest},
		{name: "fixed, exactly one test left", class: DataFixed, used: b.Fixed.DailyBytes - fixedTest, want: b.Fixed.PerTest},
		{name: "fixed, not enough for a full test", class: DataFixed, used: b.Fixed.DailyBytes - fixedTest + 1, wantErr: ErrBudgetExhausted},
		{name: "mobile disabled by default", class: DataMobile, wantErr: ErrMobileDataDisabled},
		{name: "mobile when enabled", mobileEnabled: true, class: DataMobile, want: b.Mobile.PerTest},
		{name: "mobile budget used up", mobileEnabled: true, class: DataMobile, used: b.Mobile.DailyBytes, wantErr: ErrBudgetExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := b
			budget.MobileEnabled = tt.mobileEnabled
			got, err := budget.Plan(tt.class, tt.used)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("limits = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestClassOf(t *testing.T) {
	if ClassOf(Network{Kind: ConnectionWiFi, Cost: CostMetered}) != DataMobile {
		t.Error("metered Wi-Fi (hotspot) must use the mobile budget")
	}
	if ClassOf(Network{Kind: ConnectionEthernet, Cost: CostUnmetered}) != DataFixed {
		t.Error("ethernet must use the fixed budget")
	}
}

func TestLocalTrafficSuspected(t *testing.T) {
	const test = 100 * MB
	at := func(rx, tx uint64) InterfaceCounters { return InterfaceCounters{RxBytes: rx, TxBytes: tx} }
	base := at(1_000*MB, 500*MB)

	tests := []struct {
		name  string
		after InterfaceCounters
		want  bool
	}{
		{"only the test plus protocol overhead (5%)", at(1_000*MB+75*MB, 500*MB+30*MB), false},
		{"a download running in parallel", at(1_000*MB+180*MB, 500*MB+30*MB), true},
		{"counters went backwards (adapter reset)", at(10*MB, 5*MB), false},
	}
	for _, tt := range tests {
		if got := LocalTrafficSuspected(base, tt.after, test); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}

	// Small tests: 15% of 2 MB is tiny, so the 1 MB floor avoids false alarms.
	if LocalTrafficSuspected(at(0, 0), at(2*MB+800_000, 0), 2*MB) {
		t.Error("800 KB of extra traffic on a 2 MB test must not be flagged")
	}
}

func TestDay(t *testing.T) {
	sp := time.FixedZone("BRT", -3*3600)
	// 01:30 UTC on the 6th is still the 5th in Brazil.
	at := time.Date(2026, 11, 6, 1, 30, 0, 0, time.UTC).In(sp)
	if got := DayOf(at); got != "2026-11-05" {
		t.Errorf("DayOf = %q, want 2026-11-05", got)
	}
	if !Day("2026-11-05").Valid() || Day("2026-13-01").Valid() || Day("ontem").Valid() {
		t.Error("Day.Valid misclassifies")
	}
}

func TestSpeedResultBytesUsed(t *testing.T) {
	if got := (SpeedResult{BytesDown: 3, BytesUp: 4}).BytesUsed(); got != 7 {
		t.Errorf("BytesUsed = %d, want 7", got)
	}
}
