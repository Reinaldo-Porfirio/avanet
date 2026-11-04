//go:build windows

package winnet

import (
	"errors"
	"fmt"
	"net/netip"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi        = windows.NewLazySystemDLL("iphlpapi.dll")
	icmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	icmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	icmpSendEcho    = iphlpapi.NewProc("IcmpSendEcho")
)

// icmpEchoReply is the start of ICMP_ECHO_REPLY; only these fields are read.
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32 // milliseconds
}

// ErrTimeout means no reply arrived in time (counted as packet loss).
var ErrTimeout = errors.New("icmp timeout")

// Pinger sends ICMP echo requests through the Windows ICMP API, the same one
// ping.exe uses. Unlike raw sockets it does not need administrator rights.
type Pinger struct{ handle uintptr }

func NewPinger() (*Pinger, error) {
	h, _, err := icmpCreateFile.Call()
	if windows.Handle(h) == windows.InvalidHandle {
		return nil, fmt.Errorf("IcmpCreateFile: %w", err)
	}
	return &Pinger{handle: h}, nil
}

func (p *Pinger) Close() { icmpCloseHandle.Call(p.handle) }

// Ping sends one echo request. The API reports whole milliseconds, so sub-ms
// round trips (typical to a router over cable) read as 0.
func (p *Pinger) Ping(dst netip.Addr, timeout time.Duration) (time.Duration, error) {
	if !dst.Is4() {
		return 0, fmt.Errorf("only IPv4 is supported here: %s", dst)
	}
	ip := dst.As4()
	payload := []byte("avanet-probe-0123456789abcdef")
	reply := make([]byte, 256+len(payload))

	n, _, err := icmpSendEcho.Call(
		p.handle,
		uintptr(*(*uint32)(unsafe.Pointer(&ip[0]))), // IPAddr: network byte order in memory
		uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)),
		0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)),
		uintptr(timeout.Milliseconds()))
	if n == 0 {
		if errors.Is(err, windows.Errno(11010)) { // IP_REQ_TIMED_OUT
			return 0, ErrTimeout
		}
		return 0, fmt.Errorf("IcmpSendEcho: %w", err)
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	if r.Status != 0 {
		return 0, fmt.Errorf("icmp status %d", r.Status)
	}
	return time.Duration(r.RoundTripTime) * time.Millisecond, nil
}
