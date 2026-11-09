package validate

// HasUsefulAlpha reports whether any decoded PNG pixel has alpha < 0xffff
// (including palette tRNS). Opaque RGB/truecolor without transparency returns false.
// Oversize or undecodable PNGs return false (callers should Validate for hard fails).
func HasUsefulAlpha(pngBytes []byte) bool {
	img, err := decodePNGBounded(pngBytes)
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
