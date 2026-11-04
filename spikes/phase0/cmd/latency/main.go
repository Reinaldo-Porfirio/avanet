//go:build windows

// Spike (a): which latency method works without administrator rights, and how
// do they compare? Runs 20 probes per method and target.
package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"slices"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/Reinaldo-Porfirio/avanet/spikes/phase0/internal/winnet"
)

const samples = 20

type stats struct {
	median, jitter time.Duration
	lossPct        float64
	err            error
}

func (s stats) String() string {
	if s.err != nil {
		return "ERRO: " + s.err.Error()
	}
	return fmt.Sprintf("mediana=%-8v jitter=%-8v perda=%.0f%%", s.median, s.jitter, s.lossPct)
}

// summarize computes median and jitter (mean absolute difference between
// consecutive samples, as in RFC 3550) of the successful round trips.
func summarize(rtts []time.Duration, sent int) stats {
	s := stats{lossPct: 100 * float64(sent-len(rtts)) / float64(sent)}
	if len(rtts) == 0 {
		return s
	}
	var diff time.Duration
	for i := 1; i < len(rtts); i++ {
		d := rtts[i] - rtts[i-1]
		diff += max(d, -d)
	}
	if len(rtts) > 1 {
		s.jitter = diff / time.Duration(len(rtts)-1)
	}
	sorted := slices.Clone(rtts)
	slices.Sort(sorted)
	s.median = sorted[len(sorted)/2]
	return s
}

func icmpAPI(dst netip.Addr) stats {
	p, err := winnet.NewPinger()
	if err != nil {
		return stats{err: err}
	}
	defer p.Close()
	var rtts []time.Duration
	for range samples {
		rtt, err := p.Ping(dst, time.Second)
		if err == nil {
			rtts = append(rtts, rtt)
		} else if err != winnet.ErrTimeout {
			return stats{err: err}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return summarize(rtts, samples)
}

func proBing(dst netip.Addr) stats {
	p, err := probing.NewPinger(dst.String())
	if err != nil {
		return stats{err: err}
	}
	p.SetPrivileged(true) // required on Windows
	p.Count = samples
	p.Interval = 100 * time.Millisecond
	p.Timeout = samples*100*time.Millisecond + 2*time.Second
	if err := p.Run(); err != nil {
		return stats{err: err}
	}
	st := p.Statistics()
	return summarize(st.Rtts, st.PacketsSent)
}

func tcpConnect(dst netip.Addr, port uint16) stats {
	var rtts []time.Duration
	d := net.Dialer{Timeout: time.Second}
	for range samples {
		start := time.Now()
		conn, err := d.DialContext(context.Background(), "tcp", netip.AddrPortFrom(dst, port).String())
		if err == nil {
			rtts = append(rtts, time.Since(start))
			conn.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return summarize(rtts, samples)
}

func main() {
	adapters, err := winnet.ActiveAdapters()
	if err != nil || len(adapters) == 0 {
		fmt.Fprintln(os.Stderr, "sem gateway:", err)
		os.Exit(1)
	}
	gateway := adapters[0].Gateway

	targets := []struct {
		name string
		addr netip.Addr
		port uint16
	}{
		{"roteador", gateway, 80},
		{"1.1.1.1", netip.MustParseAddr("1.1.1.1"), 443},
		{"8.8.8.8", netip.MustParseAddr("8.8.8.8"), 443},
	}
	for _, t := range targets {
		fmt.Printf("== %s (%s)\n", t.name, t.addr)
		fmt.Printf("   ICMP (API do Windows)  %v\n", icmpAPI(t.addr))
		fmt.Printf("   ICMP (pro-bing)        %v\n", proBing(t.addr))
		fmt.Printf("   TCP connect :%-5d     %v\n", t.port, tcpConnect(t.addr, t.port))
	}
}
