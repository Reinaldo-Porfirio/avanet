package probe

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// CloudflareURL is the speed test server chosen in Phase 0 (see the plan).
const CloudflareURL = "https://speed.cloudflare.com"

// transferLadder is the sequence of transfer sizes: start small so a slow
// connection wastes nothing, grow until one transfer lasts long enough.
var transferLadder = [...]int64{100_000, 1 * domain.MB, 4 * domain.MB, 10 * domain.MB, 25 * domain.MB, 50 * domain.MB}

// ErrServer means the speed test server answered something unexpected.
var ErrServer = errors.New("speed test server error")

// Speed measures download and upload throughput against a speed test server.
//
// Privacy: the Cloudflare endpoints echo the client's IP address and
// geolocation in cf-meta-* response headers. Speed never reads them; the
// server label comes from CF-RAY, which names Cloudflare's data center.
type Speed struct {
	BaseURL   string        // empty means CloudflareURL; plain http only for loopback (tests)
	UserAgent string        // empty means "AvaNet"
	Client    *http.Client  // nil means a client tuned for speed tests
	Target    time.Duration // stop growing once a transfer lasts this long; zero means 1s
	Now       func() time.Time
}

// Run performs a full test within limits. On failure the returned result
// still reports the bytes already used, so callers can charge the budget.
func (s Speed) Run(ctx context.Context, limits domain.TransferLimits) (domain.SpeedResult, error) {
	base, err := s.baseURL()
	if err != nil {
		return domain.SpeedResult{}, err
	}
	if limits.DownloadBytes < transferLadder[0] || limits.UploadBytes < transferLadder[0] {
		return domain.SpeedResult{}, fmt.Errorf("speed test limits below the smallest transfer (%d bytes): %+v", transferLadder[0], limits)
	}

	r := runner{
		base:      base,
		client:    s.Client,
		userAgent: s.UserAgent,
		target:    s.Target,
		rng:       newPayloadSource(),
	}
	if r.client == nil {
		r.client = newSpeedClient()
	}
	if r.userAgent == "" {
		r.userAgent = "AvaNet"
	}
	if r.target <= 0 {
		r.target = time.Second
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	result := domain.SpeedResult{Start: now()}

	if result.IdleLatency, err = r.idleLatency(ctx); err != nil {
		return result, err
	}
	// Opens the HTTP connection (TLS included) so the first measured transfer
	// does not pay for it, and learns which data center answers.
	if result.Server, err = r.warmUp(ctx); err != nil {
		return result, err
	}

	down, err := r.progressive(ctx, limits.DownloadBytes, r.download)
	result.DownloadMbps, result.BytesDown, result.DownloadCapped = down.mbps, down.used, down.capped
	if err != nil {
		return result, fmt.Errorf("download: %w", err)
	}

	overhead, err := r.uploadOverhead(ctx)
	if err != nil {
		return result, fmt.Errorf("upload: %w", err)
	}
	up, err := r.progressive(ctx, limits.UploadBytes, func(ctx context.Context, size int64) (transfer, error) {
		return r.upload(ctx, size, overhead)
	})
	result.UploadMbps, result.BytesUp, result.UploadCapped = up.mbps, up.used, up.capped
	if err != nil {
		return result, fmt.Errorf("upload: %w", err)
	}
	return result, nil
}

func (s Speed) baseURL() (*url.URL, error) {
	raw := s.BaseURL
	if raw == "" {
		raw = CloudflareURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("speed test URL: %w", err)
	}
	switch {
	case u.Scheme == "https":
		return u, nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return u, nil // local test servers only
	default:
		return nil, fmt.Errorf("speed test URL must use https: %q", raw)
	}
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// newSpeedClient returns a client that follows no redirects (the server must
// answer itself; a redirect could send our traffic elsewhere), asks for no
// compression (it would distort the measurement) and keeps one warm connection.
func newSpeedClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			ForceAttemptHTTP2:     true,
			DisableCompression:    true,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// runner holds the state of one test run.
type runner struct {
	base      *url.URL
	client    *http.Client
	userAgent string
	target    time.Duration
	rng       *rand.ChaCha8
}

type transfer struct {
	bytes    int64
	duration time.Duration
	mbps     float64
}

type ladderResult struct {
	mbps   float64
	used   int64
	capped bool
}

// progressive walks the transfer ladder while the next step fits in limit,
// stopping at the first transfer that lasts at least the target duration.
func (r *runner) progressive(ctx context.Context, limit int64, run func(context.Context, int64) (transfer, error)) (ladderResult, error) {
	var res ladderResult
	for _, size := range transferLadder {
		if res.used+size > limit {
			res.capped = true
			return res, nil
		}
		t, err := run(ctx, size)
		res.used += t.bytes
		if err != nil {
			return res, err
		}
		res.mbps = t.mbps
		if t.duration >= r.target {
			return res, nil
		}
	}
	res.capped = true // even the largest transfer was quicker than the target
	return res, nil
}

// samples is how many times each short calibration is repeated; the minimum
// is kept, since noise only ever adds delay.
const samples = 3

// idleLatency is the TCP handshake time to the server: one network round trip
// with no server processing. (Time to first byte of an HTTP request would add
// the server's own processing, ~40 ms on Cloudflare's speed workers.)
func (r *runner) idleLatency(ctx context.Context) (time.Duration, error) {
	host, port := r.base.Hostname(), r.base.Port()
	if port == "" {
		port = "443"
		if r.base.Scheme == "http" {
			port = "80"
		}
	}
	// Resolve once so DNS time is not counted as latency.
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return 0, fmt.Errorf("resolve %s: %w", host, errors.Join(err, errNoAddress))
	}
	target := net.JoinHostPort(addrs[0].Unmap().String(), port)

	dialer := net.Dialer{Timeout: 5 * time.Second}
	var best time.Duration
	for range samples {
		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", target)
		if err != nil {
			return 0, fmt.Errorf("idle latency: %w", err)
		}
		rtt := time.Since(start)
		if err := conn.Close(); err != nil {
			return 0, fmt.Errorf("idle latency: %w", err)
		}
		if best == 0 || rtt < best {
			best = rtt
		}
	}
	return best, nil
}

// errNoAddress is joined when a lookup succeeds without returning addresses.
var errNoAddress = errors.New("no address found")

// warmUp sends an empty download, opening the connection the transfers reuse.
func (r *runner) warmUp(ctx context.Context) (string, error) {
	resp, err := r.get(ctx, 0)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	if err := drainAndClose(resp, 0); err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	return serverLabel(resp.Header, r.base.Hostname()), nil
}

// uploadTiming is the fixed cost of an upload, measured with empty uploads,
// on each of the two clocks an upload can be timed with.
type uploadTiming struct {
	client time.Duration // request sent → response received, seen by us
	server time.Duration // the server's own receive time (Server-Timing)
	// hasServer: the server reports its timing; without it, uploads fall back
	// to the client clock.
	hasServer bool
}

// uploadOverhead times empty uploads. Their cost (a round trip, the server's
// processing) also applies to every real upload, so it is subtracted.
func (r *runner) uploadOverhead(ctx context.Context) (uploadTiming, error) {
	var best uploadTiming
	for i := range samples {
		t, err := r.timeUpload(ctx, 0)
		if err != nil {
			return uploadTiming{}, err
		}
		if i == 0 {
			best = t.uploadTiming
			continue
		}
		best.client = min(best.client, t.client)
		best.server = min(best.server, t.server)
		best.hasServer = best.hasServer && t.hasServer
	}
	return best, nil
}

func (r *runner) download(ctx context.Context, size int64) (t transfer, err error) {
	resp, err := r.get(ctx, size)
	if err != nil {
		return transfer{}, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close body: %w", closeErr)
		}
	}()

	// Read at most what was asked for, plus one byte to detect a server that
	// sends more: the budget relies on transfers never exceeding their size.
	body := io.LimitReader(resp.Body, size+1)
	points := []progressPoint{{}}
	buf := make([]byte, 64<<10)
	start := time.Now()
	var total int64
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			total += int64(n)
			points = append(points, progressPoint{at: time.Since(start), bytes: total})
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return transfer{bytes: total}, fmt.Errorf("read body: %w", readErr)
		}
	}
	if total != size {
		return transfer{bytes: total}, fmt.Errorf("%w: asked for %d bytes, got %d", ErrServer, size, total)
	}

	elapsed := points[len(points)-1].at
	return transfer{bytes: total, duration: elapsed, mbps: throughputMbps(points)}, nil
}

// upload sends size random bytes and returns the transfer rate.
//
// The server's own receive time (Server-Timing: cfSpeedWorker) is used when
// present, as Cloudflare's own speed test does: it cannot be skewed by
// buffering on our side. In Phase 3 validation it agreed with the client
// clock within 1%, which remains the fallback. (curl on Windows measured
// ~25% lower, but the server's clock showed curl itself sending slower.)
// Upload is not slow-start corrected: it is stable from 1 MB.
func (r *runner) upload(ctx context.Context, size int64, overhead uploadTiming) (transfer, error) {
	t, err := r.timeUpload(ctx, size)
	if err != nil {
		return transfer{bytes: t.sent}, err
	}
	elapsed := t.client - overhead.client
	if t.hasServer && overhead.hasServer {
		elapsed = t.server - overhead.server
	}
	elapsed = max(elapsed, time.Millisecond)
	return transfer{bytes: t.sent, duration: elapsed, mbps: mbps(t.sent, elapsed)}, nil
}

// timedUpload is the raw timing of one upload.
type timedUpload struct {
	uploadTiming
	sent int64
}

func (r *runner) timeUpload(ctx context.Context, size int64) (timedUpload, error) {
	u := r.base.JoinPath("__up")
	body := &randomReader{rng: r.rng, remaining: size}
	var reqBody io.Reader = body
	if size == 0 {
		reqBody = http.NoBody // a zero-length non-nil body would be sent chunked
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), reqBody)
	if err != nil {
		return timedUpload{}, err
	}
	req.ContentLength = size
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("Content-Type", "application/octet-stream")

	start := time.Now()
	resp, err := r.client.Do(req)
	res := timedUpload{sent: size - body.remaining}
	if err != nil {
		return res, err
	}
	res.client = time.Since(start)
	res.server, res.hasServer = serverTiming(resp.Header, "cfSpeedWorker")
	if err := drainAndClose(resp, 64<<10); err != nil {
		return res, err
	}
	return res, nil
}

// serverTiming returns the duration of one metric from Server-Timing headers
// (`name;dur=<milliseconds>[;desc=...]`, comma-separated, possibly repeated).
func serverTiming(h http.Header, metric string) (time.Duration, bool) {
	for _, header := range h.Values("Server-Timing") {
		for entry := range strings.SplitSeq(header, ",") {
			params := strings.Split(strings.TrimSpace(entry), ";")
			if !strings.EqualFold(strings.TrimSpace(params[0]), metric) {
				continue
			}
			for _, p := range params[1:] {
				value, found := strings.CutPrefix(strings.TrimSpace(p), "dur=")
				if !found {
					continue
				}
				ms, err := strconv.ParseFloat(value, 64)
				if err != nil || ms < 0 || ms > 3_600_000 {
					return 0, false // malformed or absurd: do not trust it
				}
				return time.Duration(ms * float64(time.Millisecond)), true
			}
		}
	}
	return 0, false
}

func (r *runner) get(ctx context.Context, size int64) (*http.Response, error) {
	u := r.base.JoinPath("__down")
	u.RawQuery = url.Values{"bytes": {strconv.FormatInt(size, 10)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.userAgent)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrServer, resp.Status)
	}
	return resp, nil
}

// drainAndClose reads at most limit bytes of a response we do not need, so
// the connection can be reused, and checks the status of upload responses.
func drainAndClose(resp *http.Response, limit int64) error {
	_, copyErr := io.Copy(io.Discard, io.LimitReader(resp.Body, limit))
	closeErr := resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s", ErrServer, resp.Status)
	}
	return errors.Join(copyErr, closeErr)
}

// serverLabel names the data center from CF-RAY ("<id>-<IATA code>").
func serverLabel(h http.Header, host string) string {
	if ray := h.Get("Cf-Ray"); ray != "" {
		if i := strings.LastIndexByte(ray, '-'); i >= 0 && i < len(ray)-1 {
			return "Cloudflare " + ray[i+1:]
		}
	}
	return host
}

// randomReader yields n pseudo-random bytes: incompressible, so nothing on
// the path can shrink the upload, and generated on the fly instead of keeping
// a large buffer in memory.
type randomReader struct {
	rng       *rand.ChaCha8
	remaining int64
}

func (r *randomReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, _ := r.rng.Read(p) // ChaCha8.Read always fills p and returns a nil error
	r.remaining -= int64(n)
	return n, nil
}

// newPayloadSource seeds the payload generator from the OS CSPRNG. The bytes
// only need to be incompressible, not secret; a fresh seed avoids sending the
// same bytes every time.
func newPayloadSource() *rand.ChaCha8 {
	var seed [32]byte
	_, _ = cryptorand.Read(seed[:]) // never fails (Go 1.24+: crypto/rand.Read panics instead)
	return rand.NewChaCha8(seed)
}
