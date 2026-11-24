// Package thumbnail scales an image to a small PNG with no ancillary chunks.
package thumbnail

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"

	"github.com/disintegration/imaging"
)

// MaxEdge is the longest side of an order thumbnail, in pixels.
const MaxEdge = 320

// PNG decodes src and returns a PNG whose longest edge is at most maxEdge.
// The encoding writes only critical chunks, so EXIF and text metadata are dropped.
// An image already within the limit is not enlarged.
func PNG(src []byte, maxEdge int) ([]byte, int, int, error) {
	if maxEdge < 1 {
		return nil, 0, 0, fmt.Errorf("thumbnail: max edge must be >= 1")
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("thumbnail: decode: %w", err)
	}
	b := img.Bounds()
	w, h := fitEdge(b.Dx(), b.Dy(), maxEdge)
	if w < 1 || h < 1 {
		return nil, 0, 0, fmt.Errorf("thumbnail: empty image")
	}
	if w != b.Dx() || h != b.Dy() {
		img = imaging.Resize(img, w, h, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, 0, 0, fmt.Errorf("thumbnail: encode: %w", err)
	}
	out := buf.Bytes()
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("thumbnail: encode: %w", err)
	}
	return out, cfg.Width, cfg.Height, nil
}

func fitEdge(w, h, maxEdge int) (int, int) {
	if w < 1 || h < 1 {
		return 0, 0
	}
	if w <= maxEdge && h <= maxEdge {
		return w, h
	}
	if w >= h {
		nh := int(math.Round(float64(h) * float64(maxEdge) / float64(w)))
		if nh < 1 {
			nh = 1
		}
		return maxEdge, nh
	}
	nw := int(math.Round(float64(w) * float64(maxEdge) / float64(h)))
	if nw < 1 {
		nw = 1
	}
	return nw, maxEdge
}
