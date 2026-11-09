package validate_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
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
	if len(got.Codes) != 0 || len(got.Warnings) != 0 {
		t.Fatalf("codes/warnings = %v / %v", got.Codes, got.Warnings)
	}
}

func TestValidate_DecodeFailed(t *testing.T) {
	t.Parallel()
	got := validate.Validate([]byte("not-a-png"), domain.OutputConfig{CanvasWidth: 1, CanvasHeight: 1})
	if !got.Failed() || len(got.Codes) != 1 || got.Codes[0] != validate.CodeDecodeFailed {
		t.Fatalf("got = %+v", got)
	}
	if got.Message != "png decode failed" {
		t.Fatalf("message leaked decode detail: %q", got.Message)
	}
}

func TestValidate_EmptyBytes(t *testing.T) {
	t.Parallel()
	got := validate.Validate(nil, domain.OutputConfig{CanvasWidth: 1, CanvasHeight: 1})
	if !got.Failed() || !contains(got.Codes, validate.CodeDecodeFailed) {
		t.Fatalf("got = %+v", got)
	}
}

func TestValidate_OversizeIHDR(t *testing.T) {
	t.Parallel()
	raw := rgbPNGWithDims(domain.MaxCanvasDimension+1, 1, 10, 20, 30)
	got := validate.Validate(raw, domain.OutputConfig{
		CanvasWidth: domain.MaxCanvasDimension + 1, CanvasHeight: 1, Monochrome: true,
	})
	if !got.Failed() || !contains(got.Codes, validate.CodeDecodeFailed) {
		t.Fatalf("got = %+v", got)
	}
	if validate.HasUsefulAlpha(raw) {
		t.Fatal("HasUsefulAlpha must reject oversize IHDR")
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

func TestValidate_UnsetCanvas_FailClosed(t *testing.T) {
	t.Parallel()
	pngBytes := encodePNG(t, detailedMono(16, 16, true))
	got := validate.Validate(pngBytes, domain.OutputConfig{Monochrome: true})
	if !got.Failed() || !contains(got.Codes, validate.CodeDimensionsMismatch) {
		t.Fatalf("unset canvas must fail closed, got %+v", got)
	}
}

func TestValidate_NotMonochrome(t *testing.T) {
	t.Parallel()
	img := fillRGBA(16, 16, color.RGBA{R: 255, G: 0, B: 0, A: 200})
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

func TestValidate_PremultipliedLowAlphaChroma(t *testing.T) {
	t.Parallel()
	// RGBA Set stores straight alpha; Color.RGBA() premultiplies. Low A used to
	// collapse chroma toward black and falsely pass mono — must fail after un-premultiply.
	img := fillRGBA(16, 16, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			level := (x*7 + y*11) % 16
			g := uint8(level << 4)
			img.SetRGBA(x, y, color.RGBA{R: g, G: g, B: g, A: 255})
		}
	}
	img.SetRGBA(8, 8, color.RGBA{R: 255, G: 0, B: 0, A: 8})
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 16, CanvasHeight: 16, Monochrome: true,
	})
	if !got.Failed() || !contains(got.Codes, validate.CodeNotMonochrome) {
		t.Fatalf("low-alpha chroma must fail mono, got %+v", got)
	}
}

func TestValidate_MonochromeTolerance_Boundary(t *testing.T) {
	t.Parallel()
	cfg := domain.OutputConfig{CanvasWidth: 24, CanvasHeight: 24, Monochrome: true}

	passImg := detailedMono(24, 24, true)
	passImg.SetRGBA(0, 0, color.RGBA{R: 100, G: 116, B: 100, A: 255}) // delta 16
	if got := validate.Validate(encodePNG(t, passImg), cfg); got.Failed() {
		t.Fatalf("delta==16 should pass: %+v", got)
	}

	failImg := detailedMono(24, 24, true)
	failImg.SetRGBA(0, 0, color.RGBA{R: 100, G: 117, B: 100, A: 255}) // delta 17
	if got := validate.Validate(encodePNG(t, failImg), cfg); !got.Failed() || !contains(got.Codes, validate.CodeNotMonochrome) {
		t.Fatalf("delta==17 should fail mono: %+v", got)
	}
}

func TestValidate_NoTransparency_WarningOnly_EmptyCodes(t *testing.T) {
	t.Parallel()
	pngBytes := encodePNG(t, detailedMono(32, 32, false))
	got := validate.Validate(pngBytes, domain.OutputConfig{
		CanvasWidth: 32, CanvasHeight: 32, Monochrome: true,
	})
	if got.Failed() || got.Status != validate.StatusPass {
		t.Fatalf("no_transparency must be warning, got %+v", got)
	}
	if len(got.Codes) != 0 {
		t.Fatalf("Codes must be hard-fail only, got %v", got.Codes)
	}
	if !contains(got.Warnings, validate.CodeNoTransparency) {
		t.Fatalf("warnings = %v", got.Warnings)
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

func TestValidate_FullyTransparent_Blank(t *testing.T) {
	t.Parallel()
	img := fillRGBA(16, 16, color.RGBA{R: 200, G: 10, B: 10, A: 0})
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 16, CanvasHeight: 16, Monochrome: true,
	})
	if !got.Failed() || !contains(got.Codes, validate.CodeBlankOrLowDetail) {
		t.Fatalf("fully transparent must be blank, got %+v", got)
	}
}

func TestValidate_TransparentSilhouette_PassesBlank(t *testing.T) {
	t.Parallel()
	img := fillRGBA(32, 32, color.RGBA{R: 0, G: 0, B: 0, A: 0})
	for y := 4; y < 28; y++ {
		for x := 4; x < 28; x++ {
			level := (x*7 + y*11) % 16
			g := uint8(level << 4)
			img.SetRGBA(x, y, color.RGBA{R: g, G: g, B: g, A: 255})
		}
	}
	got := validate.Validate(encodePNG(t, img), domain.OutputConfig{
		CanvasWidth: 32, CanvasHeight: 32, Monochrome: true,
	})
	if got.Failed() {
		t.Fatalf("opaque silhouette on transparent bg should pass: %+v", got)
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

// rgbPNGWithDims builds a 1-pixel-row colour-type-2 PNG with arbitrary IHDR size
// (IDAT still encodes 1×1) so DecodeConfig can reject oversize bombs.
func rgbPNGWithDims(w, h int, r, g, b byte) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	writeTestPNGChunk(&out, "IHDR", func(p *bytes.Buffer) {
		_ = binary.Write(p, binary.BigEndian, uint32(w))
		_ = binary.Write(p, binary.BigEndian, uint32(h))
		p.WriteByte(8)
		p.WriteByte(2)
		p.WriteByte(0)
		p.WriteByte(0)
		p.WriteByte(0)
	})
	var raw bytes.Buffer
	raw.WriteByte(0)
	raw.WriteByte(r)
	raw.WriteByte(g)
	raw.WriteByte(b)
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	_, _ = zw.Write(raw.Bytes())
	_ = zw.Close()
	writeTestPNGChunk(&out, "IDAT", func(p *bytes.Buffer) { p.Write(zbuf.Bytes()) })
	writeTestPNGChunk(&out, "IEND", func(p *bytes.Buffer) {})
	return out.Bytes()
}

func writeTestPNGChunk(out *bytes.Buffer, typ string, body func(*bytes.Buffer)) {
	var payload bytes.Buffer
	payload.WriteString(typ)
	body(&payload)
	_ = binary.Write(out, binary.BigEndian, uint32(payload.Len()-4))
	out.Write(payload.Bytes())
	crc := crc32.ChecksumIEEE(payload.Bytes())
	_ = binary.Write(out, binary.BigEndian, crc)
}
