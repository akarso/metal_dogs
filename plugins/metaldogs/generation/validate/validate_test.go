package validate_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/validate"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fillRGBA(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func detailedMono(w, h int, withAlpha bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			level := (x*7 + y*11) % 16
			g := uint8(level << 4)
			a := uint8(255)
			if withAlpha && (x+y)%5 == 0 {
				a = 200
			}
			img.SetRGBA(x, y, color.RGBA{R: g, G: g, B: g, A: a})
		}
	}
	return img
}

func TestValidate_Pass_DetailedMonoWithAlpha(t *testing.T) {
	t.Parallel()
	pngBytes := encodePNG(t, detailedMono(32, 32, true))
	got := validate.Validate(pngBytes, domain.OutputConfig{
		CanvasWidth: 32, CanvasHeight: 32, Monochrome: true,
	})
	if got.Failed() || got.Status != validate.StatusPass {
		t.Fatalf("got = %+v", got)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestValidate_DecodeFailed(t *testing.T) {
	t.Parallel()
	got := validate.Validate([]byte("not-a-png"), domain.OutputConfig{CanvasWidth: 1, CanvasHeight: 1})
	if !got.Failed() || len(got.Codes) != 1 || got.Codes[0] != validate.CodeDecodeFailed {
		t.Fatalf("got = %+v", got)
	}
}

func TestValidate_DimensionsMismatch(t *testing.T) {
	t.Parallel()
	pngBytes := encodePNG(t, detailedMono(16, 16, true))
	got := validate.Validate(pngBytes, domain.OutputConfig{
		CanvasWidth: 32, CanvasHeight: 16, Monochrome: true,
	})
	if !got.Failed() {
		t.Fatalf("want fail, got %+v", got)
	}
	if !contains(got.Codes, validate.CodeDimensionsMismatch) {
		t.Fatalf("codes = %v", got.Codes)
	}
}

func TestValidate_NotMonochrome(t *testing.T) {
	t.Parallel()
	img := fillRGBA(16, 16, color.RGBA{R: 255, G: 0, B: 0, A: 200})
	// Add colour variety so blank does not also fire.
	for i := 0; i < 16; i++ {
		img.SetRGBA(i, i, color.RGBA{R: uint8(i * 16), G: 0, B: uint8(255 - i*16), A: 200})
	}
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 16, CanvasHeight: 16, Monochrome: true,
	})
	if !got.Failed() || !contains(got.Codes, validate.CodeNotMonochrome) {
		t.Fatalf("got = %+v", got)
	}
}

func TestValidate_MonochromeTolerance_Pass(t *testing.T) {
	t.Parallel()
	img := detailedMono(24, 24, true)
	// Near-gray within tolerance 16.
	img.SetRGBA(0, 0, color.RGBA{R: 100, G: 110, B: 105, A: 200})
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 24, CanvasHeight: 24, Monochrome: true,
	})
	if got.Failed() {
		t.Fatalf("within tolerance should pass: %+v", got)
	}
}

func TestValidate_NoTransparency_WarningOnly(t *testing.T) {
	t.Parallel()
	pngBytes := encodePNG(t, detailedMono(32, 32, false))
	got := validate.Validate(pngBytes, domain.OutputConfig{
		CanvasWidth: 32, CanvasHeight: 32, Monochrome: true,
	})
	if got.Failed() {
		t.Fatalf("no_transparency must be warning, got %+v", got)
	}
	if !contains(got.Codes, validate.CodeNoTransparency) || !contains(got.Warnings, validate.CodeNoTransparency) {
		t.Fatalf("got = %+v", got)
	}
	if got.Status != validate.StatusPass {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestValidate_BlankOrLowDetail(t *testing.T) {
	t.Parallel()
	img := fillRGBA(16, 16, color.RGBA{R: 40, G: 40, B: 40, A: 200})
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 16, CanvasHeight: 16, Monochrome: true,
	})
	if !got.Failed() || !contains(got.Codes, validate.CodeBlankOrLowDetail) {
		t.Fatalf("got = %+v", got)
	}
}

func TestValidate_SkipsMonochromeWhenDisabled(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 200})
		}
	}
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 16, CanvasHeight: 16, Monochrome: false,
	})
	if got.Failed() || contains(got.Codes, validate.CodeNotMonochrome) {
		t.Fatalf("got = %+v", got)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
