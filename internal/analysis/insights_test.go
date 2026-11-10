package analysis

import (
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// syntheticDay is a finished day whose hours are filled by fn.
func syntheticDay(fn func(h int) Hour) *Daily {
	d := &Daily{}
	for h := range d.Hours {
		d.Hours[h] = fn(h)
		d.Coverage.Rounds += d.Hours[h].Rounds
	}
	return d
}

func goodHour() Hour {
	return Hour{Grade: domain.QualityGood, Rounds: 6, LatencyMs: 14, GatewayMs: 2, DownloadMbps: 600}
}

// eveningTrouble is a day whose 20h-22h are poor; slowRouter makes the
// router itself slow in that stretch.
func eveningTrouble(slowRouter bool) *Daily {
	return syntheticDay(func(h int) Hour {
		hour := goodHour()
		if h >= 20 && h < 22 {
			hour.Grade, hour.LatencyMs, hour.DownloadMbps = domain.QualityPoor, 110, 120
			if slowRouter {
				hour.GatewayMs = 45
			}
		}
		return hour
	})
}

func fineDay() *Daily { return syntheticDay(func(int) Hour { return goodHour() }) }

func badPatterns(r PatternReport) []Pattern {
	var bad []Pattern
	for _, p := range r.Patterns {
		if p.Kind == PatternBad {
			bad = append(bad, p)
		}
	}
	return bad
}

func TestNoPatternBeforeThreeDays(t *testing.T) {
	r := FindPatterns([]*Daily{eveningTrouble(false), eveningTrouble(false)})
	if len(r.Patterns) != 0 || r.Days != 2 {
		t.Errorf("two days: %+v", r)
	}
}

func TestRecurringEveningProblem(t *testing.T) {
	r := FindPatterns([]*Daily{eveningTrouble(false), eveningTrouble(false), eveningTrouble(false)})
	bad := badPatterns(r)
	if len(bad) != 1 {
		t.Fatalf("patterns = %+v", r.Patterns)
	}
	p := bad[0]
	if p.Range != (HourRange{From: 20, To: 22}) || p.Days != 3 || p.Hits != 3 || p.Confidence != ConfidenceLow {
		t.Errorf("pattern = %+v", p)
	}
	for _, want := range []string{"Nos últimos 3 dias", "caiu em média 80%", "das 20h às 22h", "operadora"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("text %q lacks %q", p.Text, want)
		}
	}
}

func TestOneDifferentDayIsToleratedFromFiveDays(t *testing.T) {
	r := FindPatterns([]*Daily{eveningTrouble(false), eveningTrouble(false), eveningTrouble(false), eveningTrouble(false), fineDay()})
	bad := badPatterns(r)
	if len(bad) != 1 || bad[0].Hits != 4 || bad[0].Days != 5 || bad[0].Confidence != ConfidenceMedium {
		t.Fatalf("patterns = %+v", r.Patterns)
	}
	if !strings.HasPrefix(bad[0].Text, "Em 4 dos últimos 5 dias") {
		t.Errorf("text = %q", bad[0].Text)
	}

	// With four days, all must agree.
	r = FindPatterns([]*Daily{eveningTrouble(false), eveningTrouble(false), eveningTrouble(false), fineDay()})
	if len(badPatterns(r)) != 0 {
		t.Errorf("3 of 4 days is not a pattern: %+v", r.Patterns)
	}
}

func TestSlowRouterPointsAtTheHome(t *testing.T) {
	r := FindPatterns([]*Daily{eveningTrouble(true), eveningTrouble(true), eveningTrouble(true)})
	bad := badPatterns(r)
	if len(bad) != 1 || !strings.Contains(bad[0].Text, "roteador: costuma ser o Wi-Fi") {
		t.Errorf("patterns = %+v", r.Patterns)
	}
}

func TestLatencyRiseWithoutSpeedTests(t *testing.T) {
	day := func() *Daily {
		return syntheticDay(func(h int) Hour {
			hour := goodHour()
			hour.DownloadMbps = 0
			if h >= 20 && h < 22 {
				hour.Grade, hour.LatencyMs = domain.QualityFair, 60
			}
			return hour
		})
	}
	bad := badPatterns(FindPatterns([]*Daily{day(), day(), day()}))
	if len(bad) != 1 || !strings.Contains(bad[0].Text, "ping subiu de 14 para 60 ms") {
		t.Errorf("patterns = %+v", bad)
	}
}

func TestBarelyMeasuredHoursDoNotCount(t *testing.T) {
	day := func() *Daily {
		d := eveningTrouble(false)
		for h := 20; h < 22; h++ {
			d.Hours[h].Rounds = minRoundsPerHour - 1
		}
		return d
	}
	if bad := badPatterns(FindPatterns([]*Daily{day(), day(), day()})); len(bad) != 0 {
		t.Errorf("patterns from barely measured hours: %+v", bad)
	}
}

func TestGoodStretchIsTheLongest(t *testing.T) {
	day := func() *Daily {
		return syntheticDay(func(h int) Hour {
			switch {
			case h < 6:
				return Hour{} // computer off
			case h == 12 || h >= 20:
				hour := goodHour()
				hour.Grade = domain.QualityPoor
				return hour
			default:
				return goodHour()
			}
		})
	}
	r := FindPatterns([]*Daily{day(), day(), day(), day(), day(), day()})
	var good []Pattern
	for _, p := range r.Patterns {
		if p.Kind == PatternGood {
			good = append(good, p)
		}
	}
	if len(good) != 1 || good[0].Range != (HourRange{From: 13, To: 20}) || good[0].Confidence != ConfidenceHigh {
		t.Fatalf("good = %+v", good)
	}
	if !strings.Contains(good[0].Text, "estável das 13h às 20h") {
		t.Errorf("text = %q", good[0].Text)
	}
	if len(badPatterns(r)) != maxBadPatterns {
		t.Errorf("bad patterns = %+v, want the %d longest", badPatterns(r), maxBadPatterns)
	}
}

// speedDay is a day with the given download results (upload is a tenth).
func speedDay(connection string, downloads ...float64) *Daily {
	d := &Daily{Connection: connection}
	base := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	for i, v := range downloads {
		d.Speed = append(d.Speed, SpeedPoint{At: base.Add(time.Duration(i) * time.Hour), DownloadMbps: v, UploadMbps: v / 10})
	}
	return d
}

func TestComparePlanNeedsAPlan(t *testing.T) {
	if c := ComparePlan([]*Daily{speedDay("wifi", 500)}, 0, 0); c != nil {
		t.Errorf("no plan: %+v", c)
	}
}

func TestComparePlanAgainstTheReference(t *testing.T) {
	days := []*Daily{
		speedDay("ethernet", 500, 480, 460, 150),
		speedDay("ethernet", 490, 470),
		speedDay("ethernet", 500, 500, 510, 300),
	}
	c := ComparePlan(days, 500, 50)
	if c == nil || !c.Enough || c.Days != 3 || c.Tests != 10 || c.ViaWiFi {
		t.Fatalf("comparison = %+v", c)
	}
	d := c.Download
	if d.AverageMbps != 436 || d.AveragePercent != 87.2 || !d.MeetsAverage {
		t.Errorf("average: %+v", d)
	}
	if d.MinimumMbps != 150 || d.MinimumPercent != 30 || d.BelowMinimum != 1 || d.MeetsMinimum {
		t.Errorf("minimum: %+v", d)
	}
	if c.Upload == nil || c.Upload.AveragePercent != d.AveragePercent {
		t.Errorf("upload: %+v", c.Upload)
	}
}

func TestComparePlanIsCarefulWithItsEvidence(t *testing.T) {
	days := []*Daily{speedDay("wifi", 100, 90), speedDay("wifi", 95)}
	days[0].Speed[0].LocalTraffic = true
	days[1].Speed[0].Capped = true

	c := ComparePlan(days, 100, 0)
	if c.Tests != 2 || c.Enough {
		t.Errorf("tests = %d, enough = %v: few tests give no verdict, disturbed ones do not count", c.Tests, c.Enough)
	}
	if !c.ViaWiFi || !c.Capped {
		t.Errorf("wifi = %v, capped = %v", c.ViaWiFi, c.Capped)
	}
	if c.Upload != nil {
		t.Error("no upload plan, no upload comparison")
	}
}
