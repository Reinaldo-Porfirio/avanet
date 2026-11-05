// Package ptbr formats numbers the way Brazilian Portuguese readers expect
// (decimal comma), for every user-facing text: terminal, tray and panel.
package ptbr

import (
	"strconv"
	"strings"
	"time"
)

// Decimal formats v with the given number of decimals and a decimal comma.
func Decimal(v float64, decimals int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', decimals, 64), ".", ",", 1)
}

// Millis renders a duration in milliseconds with one decimal: "12,1 ms".
func Millis(d time.Duration) string {
	return Decimal(float64(d)/float64(time.Millisecond), 1) + " ms"
}

// Mbps renders a rate: "512,3 Mbps".
func Mbps(v float64) string { return Decimal(v, 1) + " Mbps" }

// MB renders a byte count in decimal megabytes: "97,0 MB".
func MB(bytes int64) string { return Decimal(float64(bytes)/1e6, 1) + " MB" }

// Percent renders a percentage without decimals: "33%".
func Percent(p float64) string { return Decimal(p, 0) + "%" }

// Hours renders a duration as hours and minutes: "14h05".
func Hours(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	return strconv.Itoa(h) + "h" + pad2(m)
}

func pad2(v int) string {
	if v < 10 {
		return "0" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}
