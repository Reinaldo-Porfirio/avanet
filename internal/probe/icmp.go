package probe

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// ICMPEchoer pings with real ICMP packets (pro-bing).
//
// On Windows it uses a raw ICMP socket, which Windows allows for normal users
// (validated in Phase 0: no administrator needed). Elsewhere it uses
// unprivileged "ping sockets".
type ICMPEchoer struct{}

// Echo sends opts.Count requests to target and collects the replies.
func (ICMPEchoer) Echo(ctx context.Context, target netip.Addr, opts EchoOptions) (EchoResult, error) {
	if !target.IsValid() {
		return EchoResult{}, fmt.Errorf("ping: invalid target address")
	}

	p := probing.New(target.String())
	p.SetIPAddr(&net.IPAddr{IP: target.AsSlice()}) // no DNS lookup: targets are IPs
	p.SetLogger(probing.NoopLogger{})              // errors are returned, not printed
	p.SetPrivileged(runtime.GOOS == "windows")
	if target.Is4() {
		p.SetNetwork("ip4")
	} else {
		p.SetNetwork("ip6")
	}
	p.Count = opts.Count
	p.Interval = opts.Interval
	// The pinger stops once every reply arrived, or at this deadline.
	p.Timeout = time.Duration(opts.Count)*opts.Interval + opts.Wait
	p.RecordRtts = false // collected below, keyed by sequence number
	p.RecordTTLs = false

	// Replies can arrive out of order; jitter needs them in send order.
	var (
		mu    sync.Mutex
		bySeq = make(map[int]time.Duration, opts.Count)
	)
	p.OnRecv = func(pkt *probing.Packet) {
		mu.Lock()
		defer mu.Unlock()
		bySeq[pkt.Seq] = pkt.Rtt
	}

	if err := p.RunWithContext(ctx); err != nil {
		return EchoResult{}, fmt.Errorf("ping %s: %w", target, err)
	}
	if err := ctx.Err(); err != nil {
		return EchoResult{}, fmt.Errorf("ping %s: %w", target, err)
	}

	sent := p.Statistics().PacketsSent
	if sent == 0 {
		return EchoResult{}, fmt.Errorf("ping %s: %w", target, errNoEcho)
	}

	mu.Lock()
	defer mu.Unlock()
	seqs := make([]int, 0, len(bySeq))
	for seq := range bySeq {
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	rtts := make([]time.Duration, len(seqs))
	for i, seq := range seqs {
		rtts[i] = bySeq[seq]
	}
	return EchoResult{Sent: sent, RTTs: rtts}, nil
}
