package probe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// fakeSpeedServer imitates the Cloudflare endpoints on a local port.
type fakeSpeedServer struct {
	extraBytes  int64 // sent beyond what was asked (misbehaving server)
	uploadState int   // HTTP status for uploads; 0 means 200
	// serverMsPerMB, when set, makes uploads report Server-Timing like
	// Cloudflare: 40 ms of fixed processing plus this much per MB received.
	serverMsPerMB float64
	uploaded      atomic.Int64
}

func (f *fakeSpeedServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__down", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "bad bytes", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cf-Ray", "8f1e2d3c4b5a6978-TST")
		w.Header().Set("Content-Type", "application/octet-stream")
		if n > 0 {
			// Like a real network, the body takes a moment after the headers;
			// on loopback it would otherwise arrive below clock resolution.
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			time.Sleep(2 * time.Millisecond)
		}
		_, _ = io.CopyN(w, zeroReader{}, n+f.extraBytes)
	})
	mux.HandleFunc("POST /__up", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		f.uploaded.Add(n)
		if f.serverMsPerMB > 0 {
			dur := 40 + f.serverMsPerMB*float64(n)/domain.MB
			w.Header().Set("Server-Timing", "cfSpeedEdge;dur=9, cfSpeedWorker;dur="+strconv.FormatFloat(dur, 'f', 3, 64))
		}
		if f.uploadState != 0 && n > 0 { // calibration (empty) uploads succeed
			w.WriteHeader(f.uploadState)
		}
	})
	return mux
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func startFakeServer(t *testing.T, f *fakeSpeedServer) string {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestSpeedRunStopsWhenTransferIsLongEnough(t *testing.T) {
	fake := &fakeSpeedServer{}
	s := Speed{BaseURL: startFakeServer(t, fake), Target: time.Nanosecond}

	res, err := s.Run(context.Background(), domain.TransferLimits{DownloadBytes: 6 * domain.MB, UploadBytes: 2 * domain.MB})
	if err != nil {
		t.Fatal(err)
	}
	// Any transfer lasts more than 1ns: only the first ladder step runs.
	if res.BytesDown != transferLadder[0] || res.BytesUp != transferLadder[0] {
		t.Errorf("bytes down/up = %d/%d, want %d each", res.BytesDown, res.BytesUp, transferLadder[0])
	}
	if res.DownloadCapped || res.UploadCapped {
		t.Error("test reached its target duration; it must not be flagged as capped")
	}
	if res.DownloadMbps <= 0 || res.UploadMbps <= 0 {
		t.Errorf("throughput down/up = %v/%v, want > 0", res.DownloadMbps, res.UploadMbps)
	}
	if res.Server != "Cloudflare TST" {
		t.Errorf("Server = %q, want the data center from CF-RAY", res.Server)
	}
	if got := fake.uploaded.Load(); got != res.BytesUp {
		t.Errorf("server received %d bytes, result says %d", got, res.BytesUp)
	}
}

// TestSpeedRunTimesUploadsWithServerClock checks that, when the server reports
// its receive time, the upload rate comes from it minus the fixed processing
// measured on empty uploads, not from the (optimistic) client clock.
func TestSpeedRunTimesUploadsWithServerClock(t *testing.T) {
	// 100 ms per MB on the server = 80 Mbps, whatever loopback really does.
	fake := &fakeSpeedServer{serverMsPerMB: 100}
	s := Speed{BaseURL: startFakeServer(t, fake), Target: time.Nanosecond}

	res, err := s.Run(context.Background(), domain.DefaultDataBudget().Mobile.PerTest)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(res.UploadMbps-80) > 0.01 {
		t.Errorf("UploadMbps = %v, want 80 from the server's timing", res.UploadMbps)
	}
}

func TestServerTiming(t *testing.T) {
	h := http.Header{}
	h.Add("Server-Timing", `cfSpeedEdge;dur=9, cfSpeedWorker;dur=1352.5`)
	h.Add("Server-Timing", `cfL4;desc="?proto=TCP&rtt=13216"`)

	if d, ok := serverTiming(h, "cfSpeedWorker"); !ok || d != 1352500*time.Microsecond {
		t.Errorf("cfSpeedWorker = %v, %v; want 1352.5ms", d, ok)
	}
	if _, ok := serverTiming(h, "cfL4"); ok {
		t.Error("a metric without dur must not be reported")
	}
	if _, ok := serverTiming(h, "missing"); ok {
		t.Error("missing metric reported")
	}
	bad := http.Header{"Server-Timing": {"cfSpeedWorker;dur=-5", "cfSpeedWorker;dur=abc"}}
	if _, ok := serverTiming(bad, "cfSpeedWorker"); ok {
		t.Error("malformed durations must be rejected")
	}
}

func TestSpeedRunNeverExceedsLimits(t *testing.T) {
	fake := &fakeSpeedServer{}
	s := Speed{BaseURL: startFakeServer(t, fake), Target: time.Hour} // never "long enough"
	limits := domain.TransferLimits{DownloadBytes: 6 * domain.MB, UploadBytes: 2 * domain.MB}

	res, err := s.Run(context.Background(), limits)
	if err != nil {
		t.Fatal(err)
	}
	// Ladder 0.1 + 1 + 4 MB fits in 6 MB; the next step (10 MB) does not.
	if want := int64(5_100_000); res.BytesDown != want {
		t.Errorf("BytesDown = %d, want %d", res.BytesDown, want)
	}
	if want := int64(1_100_000); res.BytesUp != want {
		t.Errorf("BytesUp = %d, want %d", res.BytesUp, want)
	}
	if !res.DownloadCapped || !res.UploadCapped {
		t.Error("stopping at the byte cap must be flagged")
	}
	if res.BytesDown > limits.DownloadBytes || res.BytesUp > limits.UploadBytes {
		t.Error("limits exceeded")
	}
}

func TestSpeedRunRejectsOversizedResponse(t *testing.T) {
	s := Speed{BaseURL: startFakeServer(t, &fakeSpeedServer{extraBytes: 10}), Target: time.Nanosecond}
	res, err := s.Run(context.Background(), domain.DefaultDataBudget().Mobile.PerTest)
	if !errors.Is(err, ErrServer) {
		t.Fatalf("error = %v, want ErrServer", err)
	}
	if res.BytesDown > transferLadder[0]+1 {
		t.Errorf("read %d bytes; must stop right after the requested size", res.BytesDown)
	}
}

func TestSpeedRunReportsBytesUsedOnFailure(t *testing.T) {
	s := Speed{BaseURL: startFakeServer(t, &fakeSpeedServer{uploadState: http.StatusInternalServerError}), Target: time.Nanosecond}
	res, err := s.Run(context.Background(), domain.DefaultDataBudget().Mobile.PerTest)
	if !errors.Is(err, ErrServer) {
		t.Fatalf("error = %v, want ErrServer", err)
	}
	if res.BytesDown == 0 || res.BytesUp == 0 {
		t.Errorf("bytes used must be reported even on failure, got down=%d up=%d", res.BytesDown, res.BytesUp)
	}
}

func TestSpeedRunDoesNotFollowRedirects(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect was followed to another server")
	}))
	t.Cleanup(elsewhere.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	_, err := Speed{BaseURL: redirector.URL}.Run(context.Background(), domain.DefaultDataBudget().Mobile.PerTest)
	if !errors.Is(err, ErrServer) {
		t.Errorf("error = %v, want ErrServer", err)
	}
}

func TestSpeedRunValidatesConfiguration(t *testing.T) {
	ctx := context.Background()
	limits := domain.DefaultDataBudget().Mobile.PerTest
	for _, base := range []string{"http://speed.example.com", "ftp://127.0.0.1", "://bad"} {
		if _, err := (Speed{BaseURL: base}).Run(ctx, limits); err == nil {
			t.Errorf("BaseURL %q accepted, want error", base)
		}
	}
	if _, err := (Speed{BaseURL: "http://127.0.0.1:1"}).Run(ctx, domain.TransferLimits{DownloadBytes: 10, UploadBytes: 10}); err == nil {
		t.Error("limits below the smallest transfer accepted")
	}
}

func TestSpeedRunHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Speed{BaseURL: startFakeServer(t, &fakeSpeedServer{})}.Run(ctx, domain.DefaultDataBudget().Mobile.PerTest)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestThroughputMbps(t *testing.T) {
	ms := func(v int) time.Duration { return time.Duration(v) * time.Millisecond }

	// 1 MB in 100 ms is too short for a steady window: plain average.
	short := []progressPoint{{0, 0}, {ms(50), 500_000}, {ms(100), 1_000_000}}
	if got := throughputMbps(short); math.Abs(got-80) > 0.01 {
		t.Errorf("short transfer = %v Mbps, want 80", got)
	}

	// Slow start: 1 MB in the first 300 ms, then 10 MB/s. The steady window
	// (after 30% of 1 s) must report 80 Mbps, not the 61.6 Mbps average.
	rampUp := []progressPoint{{0, 0}, {ms(300), 1_000_000}, {ms(650), 4_500_000}, {ms(1000), 8_000_000}}
	if got := throughputMbps(rampUp); math.Abs(got-80) > 0.01 {
		t.Errorf("ramp-up transfer = %v Mbps, want 80", got)
	}

	if got := throughputMbps(nil); got != 0 {
		t.Errorf("no points = %v, want 0", got)
	}
}

func TestRandomReaderYieldsExactlyNBytes(t *testing.T) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, &randomReader{rng: newPayloadSource(), remaining: 100_003})
	if err != nil || n != 100_003 {
		t.Fatalf("copied %d bytes, err %v; want 100003", n, err)
	}
	if bytes.Count(buf.Bytes(), []byte{0}) > 2_000 {
		t.Error("payload looks compressible (too many zero bytes)")
	}
}
