package mockup

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

//go:embed testdata/black-tee.png
var defaultBasePNG []byte

// DefaultBasePNG is the embedded black-tee placeholder used when no override is configured.
// Current fixture is 80×100 — a stand-in until a real web-sized garment base lands.
func DefaultBasePNG() []byte {
	return append([]byte(nil), defaultBasePNG...)
}

// DefaultPrintArea returns the artwork placement on DefaultBasePNG (80×100).
func DefaultPrintArea() domain.PrintArea {
	return domain.PrintArea{X: 20, Y: 30, Width: 40, Height: 40}
}

// MaxPreviewEdge caps the longest side of the composed mockup for web delivery.
// No-op while the embedded base is smaller than this edge.
const MaxPreviewEdge = 1024

// Compose draws artworkPNG into place on basePNG and returns a PNG preview.
// Artwork is scaled with aspect preserved (contain) and centered in place.
// The embedded default base is a small placeholder; callers must not treat it as
// production-ready web marketing art until a larger base ships.
func Compose(artworkPNG, basePNG []byte, place domain.PrintArea) ([]byte, error) {
	if len(artworkPNG) == 0 {
		return nil, fmt.Errorf("mockup: artwork required")
	}
	if len(basePNG) == 0 {
		return nil, fmt.Errorf("mockup: base required")
	}
	if place.Width <= 0 || place.Height <= 0 {
		return nil, fmt.Errorf("mockup: print area width/height must be > 0")
	}
	if place.X < 0 || place.Y < 0 {
		return nil, fmt.Errorf("mockup: print area origin must be >= 0")
	}

	artCfg, _, err := image.DecodeConfig(bytes.NewReader(artworkPNG))
	if err != nil {
		return nil, fmt.Errorf("mockup: decode artwork config: %w", err)
	}
	if artCfg.Width <= 0 || artCfg.Height <= 0 ||
		artCfg.Width > domain.MaxCanvasDimension || artCfg.Height > domain.MaxCanvasDimension {
		return nil, fmt.Errorf("mockup: artwork dimensions out of range")
	}
	baseCfg, _, err := image.DecodeConfig(bytes.NewReader(basePNG))
	if err != nil {
		return nil, fmt.Errorf("mockup: decode base config: %w", err)
	}
	if baseCfg.Width <= 0 || baseCfg.Height <= 0 ||
		baseCfg.Width > domain.MaxCanvasDimension || baseCfg.Height > domain.MaxCanvasDimension {
		return nil, fmt.Errorf("mockup: base dimensions out of range")
	}
	if int64(place.X)+int64(place.Width) > int64(baseCfg.Width) ||
		int64(place.Y)+int64(place.Height) > int64(baseCfg.Height) {
		return nil, fmt.Errorf("mockup: print area %v outside base %dx%d", place, baseCfg.Width, baseCfg.Height)
	}

	art, err := png.Decode(bytes.NewReader(artworkPNG))
	if err != nil {
		return nil, fmt.Errorf("mockup: decode artwork: %w", err)
	}
	base, err := png.Decode(bytes.NewReader(basePNG))
	if err != nil {
		return nil, fmt.Errorf("mockup: decode base: %w", err)
	}
	baseBounds := base.Bounds()

	out := image.NewRGBA(baseBounds)
	draw.Draw(out, out.Bounds(), base, baseBounds.Min, draw.Src)

	scaled := scaleContain(art, place.Width, place.Height)
	sb := scaled.Bounds()
	ox := place.X + (place.Width-sb.Dx())/2
	oy := place.Y + (place.Height-sb.Dy())/2
	target := image.Rect(ox, oy, ox+sb.Dx(), oy+sb.Dy())
	draw.Draw(out, target, scaled, sb.Min, draw.Over)

	preview := downscale(out, MaxPreviewEdge)
	var buf bytes.Buffer
	if err := png.Encode(&buf, preview); err != nil {
		return nil, fmt.Errorf("mockup: encode: %w", err)
	}
	return buf.Bytes(), nil
}

func scaleContain(src image.Image, maxW, maxH int) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw <= 0 || sh <= 0 || maxW <= 0 || maxH <= 0 {
		return src
	}
	scale := math.Min(float64(maxW)/float64(sw), float64(maxH)/float64(sh))
	dw := int(math.Round(float64(sw) * scale))
	dh := int(math.Round(float64(sh) * scale))
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	if dw > maxW {
		dw = maxW
	}
	if dh > maxH {
		dh = maxH
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := sb.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			sx := sb.Min.X + x*sw/dw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func downscale(src *image.RGBA, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxEdge <= 0 || (w <= maxEdge && h <= maxEdge) {
		return src
	}
	return scaleContain(src, maxEdge, maxEdge)
}
