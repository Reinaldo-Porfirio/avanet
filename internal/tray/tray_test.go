package tray

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

func TestIconIsValidICOForEveryQuality(t *testing.T) {
	seen := map[string]domain.Quality{}
	for q := range qualityColors {
		data, err := icon(q)
		if err != nil {
			t.Fatal(err)
		}
		var header [3]uint16
		if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &header); err != nil {
			t.Fatal(err)
		}
		if header != [3]uint16{0, 1, uint16(len(iconSizes))} {
			t.Errorf("%v: ICO header = %v", q, header)
		}
		// The first image must decode as a PNG of the declared size.
		offset := binary.LittleEndian.Uint32(data[6+12:])
		img, err := png.Decode(bytes.NewReader(data[offset:]))
		if err != nil {
			t.Fatalf("%v: first image: %v", q, err)
		}
		if img.Bounds().Dx() != iconSizes[0] {
			t.Errorf("%v: first image is %dpx, want %d", q, img.Bounds().Dx(), iconSizes[0])
		}
		if other, dup := seen[string(data)]; dup {
			t.Errorf("%v and %v share the same icon", q, other)
		}
		seen[string(data)] = q
	}
}

func TestICORejectsInvalidInput(t *testing.T) {
	data := []byte{1}
	for name, tc := range map[string]struct {
		sizes  []int
		images [][]byte
	}{
		"no images":      {nil, nil},
		"size mismatch":  {[]int{16, 32}, [][]byte{data}},
		"side too large": {[]int{512}, [][]byte{data}},
		"empty image":    {[]int{16}, [][]byte{{}}},
	} {
		if _, err := ico(tc.sizes, tc.images); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDrawLeavesCornersTransparentAndDrawsThePulse(t *testing.T) {
	img := draw(32, qualityColors[domain.QualityGood])
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want transparent (round icon)", a)
	}
	white := 0
	for y := range 32 {
		for x := range 32 {
			if c := img.NRGBAAt(x, y); c.R > 240 && c.G > 240 && c.B > 240 && c.A > 200 {
				white++
			}
		}
	}
	if white < 20 {
		t.Errorf("only %d white pixels: the pulse line is missing", white)
	}
}

func TestStatusTexts(t *testing.T) {
	stats, err := domain.SummarizeLatency([]time.Duration{11 * time.Millisecond}, 1)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 21, 3, 0, 0, time.Local)

	tests := []struct {
		name                  string
		status                agent.Status
		wantStatus, wantSpeed string
	}{
		{"before the first round", agent.Status{}, "Aguardando a primeira medição…", "Nenhum teste de velocidade ainda"},
		{"normal", agent.Status{At: at, Internet: stats, BestTarget: "Quad9", Quality: domain.QualityGood,
			LastSpeed: &domain.SpeedResult{Start: at, DownloadMbps: 700.4, UploadMbps: 99.2}},
			"Internet: 11,0 ms · boa", "Velocidade: 700 ↓ / 99 ↑ Mbps às 21:03"},
		{"outage", agent.Status{At: at, Outage: true, Quality: domain.QualityPoor}, "Sem conexão com a internet", "Nenhum teste de velocidade ainda"},
		{"notice wins", agent.Status{At: at, BestTarget: "Quad9", Internet: stats, SpeedNotice: "Orçamento esgotado"},
			"Internet: 11,0 ms · sem dados", "Orçamento esgotado"},
	}
	for _, tt := range tests {
		if got := statusLine(&tt.status); got != tt.wantStatus {
			t.Errorf("%s: statusLine = %q, want %q", tt.name, got, tt.wantStatus)
		}
		if got := speedLine(&tt.status); got != tt.wantSpeed {
			t.Errorf("%s: speedLine = %q, want %q", tt.name, got, tt.wantSpeed)
		}
		if tip := tooltip(&tt.status); !strings.HasPrefix(tip, "AvaNet · ") || len([]rune(tip)) > maxTooltip {
			t.Errorf("%s: tooltip %q", tt.name, tip)
		}
	}
}
