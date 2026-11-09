package domain

import "time"

// LatencyRecord is one target's result in one recorded latency round.
type LatencyRecord struct {
	At     time.Time
	Target string
	Stats  LatencyStats
}

// TimeSpan is a period of time; End is exclusive.
type TimeSpan struct {
	Start, End time.Time
}

// WiFiRecord is a recorded reading of the Wi-Fi signal.
type WiFiRecord struct {
	At            time.Time
	SignalPercent int
}

// NetworkRef identifies the network a day's measurements came from.
type NetworkRef struct {
	Kind ConnectionKind
	SSID string
}

// DayData is everything recorded in one calendar day: the raw material of
// the daily report. Records are ordered by time.
type DayData struct {
	Latency  []LatencyRecord
	Speed    []SpeedResult
	WiFi     []WiFiRecord
	Outages  []Outage   // End is zero if it never closed
	Sessions []TimeSpan // when the agent was running
	DataUsed int64      // bytes spent on speed tests
	// Network is the network used for most of the day, if any is known.
	Network    NetworkRef
	HasNetwork bool
}
