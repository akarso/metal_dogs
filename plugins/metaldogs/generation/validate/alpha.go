package validate

import (
	"bytes"
	"image/png"
)

// HasUsefulAlpha reports whether any decoded PNG pixel has alpha < 0xffff
// (including palette tRNS). Opaque RGB/truecolor without transparency returns false.
func HasUsefulAlpha(pngBytes []byte) bool {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return false
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0xffff {
				return true
			}
		}
	}
	return false
}
