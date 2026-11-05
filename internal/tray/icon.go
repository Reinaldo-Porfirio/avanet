package tray

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// Quality colors from the visual concept (docs/design), slightly brighter so
// they read on both light and dark taskbars.
var qualityColors = map[domain.Quality]color.NRGBA{
	domain.QualityGood:    {R: 0x2E, G: 0x8A, B: 0x78, A: 0xFF},
	domain.QualityFair:    {R: 0xC8, G: 0x92, B: 0x2E, A: 0xFF},
	domain.QualityPoor:    {R: 0xC9, G: 0x47, B: 0x2E, A: 0xFF},
	domain.QualityUnknown: {R: 0x6D, G: 0x71, B: 0x78, A: 0xFF},
}

// pulse is the AvaNet mark (the "A" that is also a signal spike), in a
// 32-unit box, as in docs/design/conceito-relatorio.html.
var pulse = [][2]float64{{3, 22}, {9, 22}, {13, 7}, {19, 28}, {23, 18}, {29, 18}}

// iconSizes are embedded in each .ico; Windows picks one by display scale.
var iconSizes = []int{16, 32, 48}

// icon returns a Windows .ico (PNG images inside, supported since Vista) of
// a disc in the quality's color with the white pulse on top.
func icon(q domain.Quality) ([]byte, error) {
	fill, ok := qualityColors[q]
	if !ok {
		fill = qualityColors[domain.QualityUnknown]
	}

	images := make([][]byte, len(iconSizes))
	for i, size := range iconSizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, draw(size, fill)); err != nil {
			return nil, fmt.Errorf("encode %dpx icon: %w", size, err)
		}
		images[i] = buf.Bytes()
	}
	return ico(iconSizes, images)
}

// draw rasterizes the icon with simple anti-aliasing: each pixel's coverage
// is estimated from its distance to the shape's edge.
func draw(size int, fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	center, radius := s/2, s/2-0.5
	stroke := math.Max(1.25, s/12) // line width in pixels
	inset := s * 0.16              // keep the pulse inside the disc

	for y := range size {
		for x := range size {
			px, py := float64(x)+0.5, float64(y)+0.5
			disc := coverage(radius - math.Hypot(px-center, py-center))
			if disc == 0 {
				continue
			}
			line := coverage(stroke/2 - distanceToPulse(px, py, s, inset))
			r := mix(fill.R, 0xFF, line)
			g := mix(fill.G, 0xFF, line)
			b := mix(fill.B, 0xFF, line)
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: uint8(math.Round(disc * 255))})
		}
	}
	return img
}

// coverage turns a signed distance (positive = inside) into opacity 0..1.
func coverage(signed float64) float64 { return math.Max(0, math.Min(1, signed+0.5)) }

func mix(from, to uint8, t float64) uint8 {
	return uint8(math.Round(float64(from) + (float64(to)-float64(from))*t))
}

func distanceToPulse(px, py, size, inset float64) float64 {
	scale := (size - 2*inset) / 32
	best := math.Inf(1)
	for i := 1; i < len(pulse); i++ {
		ax, ay := inset+pulse[i-1][0]*scale, inset+pulse[i-1][1]*scale
		bx, by := inset+pulse[i][0]*scale, inset+pulse[i][1]*scale
		best = math.Min(best, distanceToSegment(px, py, ax, ay, bx, by))
	}
	return best
}

func distanceToSegment(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// ICO format limits.
const (
	icoHeaderSize = 6
	icoEntrySize  = 16
	icoMaxImages  = 255     // kept well below the format's uint16 count
	icoMaxSide    = 256     // stored as 0 in the 8-bit width/height fields
	icoMaxBytes   = 1 << 24 // sanity cap per image; tray icons are a few KB
)

type icoEntry struct {
	Width, Height, Colors, Reserved uint8
	Planes, BitsPerPixel            uint16
	Size, Offset                    uint32
}

// ico packs PNG images into the ICO container: a 6-byte header, a 16-byte
// directory entry per image, then the image data. Every size is validated
// first, so the narrowing conversions below cannot overflow.
func ico(sizes []int, images [][]byte) ([]byte, error) {
	if len(images) == 0 || len(images) > icoMaxImages || len(sizes) != len(images) {
		return nil, fmt.Errorf("ico: need 1-%d images with one size each, got %d images and %d sizes", icoMaxImages, len(images), len(sizes))
	}
	entries := make([]icoEntry, len(images))
	offset := uint32(icoHeaderSize + icoEntrySize*len(images)) //nolint:gosec // len(images) <= 255, checked above
	for i, data := range images {
		side, length := sizes[i], len(data)
		if side < 1 || side > icoMaxSide || length == 0 || length > icoMaxBytes {
			return nil, fmt.Errorf("ico: image %d has side %d and %d bytes", i, side, length)
		}
		dim := uint8(side % icoMaxSide) // 256 is stored as 0
		size := uint32(length)
		entries[i] = icoEntry{Width: dim, Height: dim, Planes: 1, BitsPerPixel: 32, Size: size, Offset: offset}
		offset += size
	}

	var out bytes.Buffer
	header := [3]uint16{0, 1, uint16(len(images))} //nolint:gosec // <= 255 checked above; type 1 = icon
	if err := binary.Write(&out, binary.LittleEndian, header); err != nil {
		return nil, err
	}
	if err := binary.Write(&out, binary.LittleEndian, entries); err != nil {
		return nil, err
	}
	for _, data := range images {
		out.Write(data)
	}
	return out.Bytes(), nil
}
