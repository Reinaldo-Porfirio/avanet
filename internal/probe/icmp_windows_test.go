package probe

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

// TestICMPEchoerLoopback sends real ICMP packets to 127.0.0.1, which needs no
// network, to prove the Windows path works without administrator rights.
func TestICMPEchoerLoopback(t *testing.T) {
	opts := EchoOptions{Count: 3, Interval: 50 * time.Millisecond, Wait: time.Second}
	res, err := ICMPEchoer{}.Echo(context.Background(), netip.MustParseAddr("127.0.0.1"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != opts.Count {
		t.Errorf("Sent = %d, want %d", res.Sent, opts.Count)
	}
	if len(res.RTTs) == 0 {
		t.Error("loopback did not answer")
	}
}
