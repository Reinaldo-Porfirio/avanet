// Spike (c): measure download/upload against Cloudflare's speed test endpoints
// with growing transfer sizes and a byte cap, and compare with a heavier
// multi-stream reference to see how much the capped test underestimates.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	base      = "https://speed.cloudflare.com"
	userAgent = "AvaNet-spike/0.1"
	// Stop growing once a transfer lasts this long: long enough for TCP to
	// ramp up, short enough not to waste data.
	targetDuration = 1500 * time.Millisecond
)

var client = &http.Client{Timeout: 60 * time.Second}

type result struct {
	bytes    int64
	duration time.Duration
}

func (r result) mbps() float64 { return float64(r.bytes*8) / r.duration.Seconds() / 1e6 }

// download measures from the moment headers arrive to the end of the body,
// excluding connection setup and server processing time.
func download(ctx context.Context, size int64) (result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/__down?bytes=%d", base, size), nil)
	if err != nil {
		return result{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result{}, fmt.Errorf("status %s", resp.Status)
	}
	start := time.Now()
	n, err := io.Copy(io.Discard, resp.Body)
	return result{bytes: n, duration: time.Since(start)}, err
}

// upload measures the whole request minus one round trip (estimated by the
// time a tiny upload takes), since the server answers only after the body.
func upload(ctx context.Context, payload []byte, rtt time.Duration) (result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/__up", bytes.NewReader(payload))
	if err != nil {
		return result{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/octet-stream")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return result{}, fmt.Errorf("status %s", resp.Status)
	}
	return result{bytes: int64(len(payload)), duration: max(time.Since(start)-rtt, time.Millisecond)}, nil
}

func progressive(ctx context.Context, name string, sizes []int64, run func(int64) (result, error)) (total int64) {
	fmt.Printf("\n%s progressivo (para quando uma transferência passa de %v):\n", name, targetDuration)
	for _, size := range sizes {
		r, err := run(size)
		if err != nil {
			fmt.Println("   ERRO:", err)
			return total
		}
		total += r.bytes
		fmt.Printf("   %6.1f MB em %-12v -> %7.1f Mbps\n", float64(r.bytes)/1e6, r.duration.Round(time.Millisecond), r.mbps())
		if r.duration >= targetDuration {
			break
		}
	}
	fmt.Printf("   dados gastos: %.1f MB\n", float64(total)/1e6)
	return total
}

// parallel runs n transfers at once and reports aggregate throughput.
func parallel(n int, run func() (result, error)) (result, error) {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int64
		first error
	)
	start := time.Now()
	for range n {
		wg.Go(func() {
			r, err := run()
			mu.Lock()
			defer mu.Unlock()
			total += r.bytes
			if err != nil && first == nil {
				first = err
			}
		})
	}
	wg.Wait()
	return result{bytes: total, duration: time.Since(start)}, first
}

func main() {
	ctx := context.Background()
	const mb = 1_000_000

	// Round trip estimate: smallest possible upload.
	var rtt time.Duration
	for range 3 {
		start := time.Now()
		if _, err := upload(ctx, []byte{0}, 0); err != nil {
			fmt.Fprintln(os.Stderr, "falha ao contatar a Cloudflare:", err)
			os.Exit(1)
		}
		if d := time.Since(start); rtt == 0 || d < rtt {
			rtt = d
		}
	}
	fmt.Printf("RTT estimado até speed.cloudflare.com: %v\n", rtt.Round(time.Millisecond))

	payload := make([]byte, 100*mb)
	_, _ = rand.Read(payload) // random: nothing along the way can compress it

	spent := progressive(ctx, "Download", []int64{100_000, 1 * mb, 10 * mb, 25 * mb, 50 * mb},
		func(size int64) (result, error) { return download(ctx, size) })
	spent += progressive(ctx, "Upload", []int64{100_000, 1 * mb, 10 * mb, 25 * mb},
		func(size int64) (result, error) { return upload(ctx, payload[:size], rtt) })

	fmt.Println("\nReferência pesada (4 conexões em paralelo):")
	if r, err := parallel(4, func() (result, error) { return download(ctx, 50*mb) }); err == nil {
		spent += r.bytes
		fmt.Printf("   download: %.0f MB -> %.1f Mbps\n", float64(r.bytes)/1e6, r.mbps())
	} else {
		fmt.Println("   download ERRO:", err)
	}
	if r, err := parallel(4, func() (result, error) { return upload(ctx, payload[:25*mb], rtt) }); err == nil {
		spent += r.bytes
		fmt.Printf("   upload:   %.0f MB -> %.1f Mbps\n", float64(r.bytes)/1e6, r.mbps())
	} else {
		fmt.Println("   upload ERRO:", err)
	}
	fmt.Printf("\nTotal de dados usados pelo spike: %.0f MB\n", float64(spent)/1e6)
	fmt.Println(strings.Repeat("-", 40))
}
