package production

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPrepare_Rules(t *testing.T) {
	t.Parallel()
	spec := testSpec()
	cases := []struct {
		name string
		png  []byte
		spec Spec
		code string
	}{
		{name: "empty", png: nil, code: CodeArtworkUnreadable},
		{name: "garbage", png: []byte("not-a-png"), code: CodeArtworkUnreadable},
		{name: "jpeg", png: []byte{0xff, 0xd8, 0xff, 0xd9}, code: CodeArtworkUnreadable},
		{name: "truncated", png: mustPNG(t, 8, 8, false)[:12], code: CodeArtworkUnreadable},
		{name: "aspect", png: mustPNG(t, 30, 10, false), code: CodeAspectMismatch},
		{name: "aspect just over 1pct", png: mustPNG(t, 100, 99, false), spec: specSize(100, 100), code: CodeAspectMismatch},
		{name: "too small reject", png: mustPNG(t, 10, 10, false), spec: specSize(20, 20), code: CodeTooSmall},
		{name: "one axis small reject", png: mustPNG(t, 198, 200, false), spec: specSize(200, 200), code: CodeTooSmall},
		{name: "transparency", png: mustPNG(t, 20, 20, false), spec: specTransparency(specSize(20, 20)), code: CodeTransparencyRequired},
		{name: "opaque palette", png: mustPalette(t, 16, 16, false), spec: specTransparency(specSize(16, 16)), code: CodeTransparencyRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := tc.spec
			if s.WidthPx == 0 {
				s = spec
			}
			_, err := Prepare(tc.png, s)
			var pe *Error
			if !errors.As(err, &pe) || pe.Code != tc.code {
				t.Fatalf("err = %v, want code %s", err, tc.code)
			}
		})
	}
}

func TestPrepare_ExactAndDownscale(t *testing.T) {
	t.Parallel()
	exact := testSpec()
	out, err := Prepare(mustPNG(t, exact.WidthPx, exact.HeightPx, false), exact)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, exact.WidthPx, exact.HeightPx)
	assertMeta(t, out.PNG, exact.DPI)

	down := specSize(20, 10)
	out, err = Prepare(mustPNG(t, 40, 20, false), down)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, 20, 10)
	assertMeta(t, out.PNG, down.DPI)
}

func TestPrepare_AspectBoundaryInclusive(t *testing.T) {
	t.Parallel()
	spec := specSize(100, 100)
	out, err := Prepare(mustPNG(t, 101, 100, false), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, 100, 100)

	out, err = Prepare(mustPNG(t, 100, 101, false), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, 100, 100)
}

func TestPrepare_Upscale(t *testing.T) {
	t.Parallel()
	spec := specSize(20, 20)
	spec.UpscalePolicy = PolicyUpscale
	out, err := Prepare(mustPNG(t, 10, 10, true), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, 20, 20)
	if !pngAlpha(t, out.PNG) {
		t.Fatal("upscaled output lost alpha")
	}

	mixed := specSize(200, 200)
	mixed.UpscalePolicy = PolicyUpscale
	out, err = Prepare(mustPNG(t, 198, 200, false), mixed)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, 200, 200)
}

func TestPrepare_TransparencyAccepted(t *testing.T) {
	t.Parallel()
	spec := specTransparency(specSize(16, 16))
	out, err := Prepare(mustPNG(t, 16, 16, true), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, 16, 16)
	if !pngAlpha(t, out.PNG) {
		t.Fatal("output has no alpha")
	}

	out, err = Prepare(mustPalette(t, 16, 16, true), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !pngAlpha(t, out.PNG) {
		t.Fatal("palette tRNS was not treated as alpha")
	}
}

func TestPrepare_NoCropNoPad(t *testing.T) {
	t.Parallel()
	const w, h = 40, 20
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= w-4 {
				img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	raw := encodePNG(t, img)
	spec := specSize(w/2, h/2)
	out, err := Prepare(raw, spec)
	if err != nil {
		t.Fatal(err)
	}
	assertSize(t, out.PNG, spec.WidthPx, spec.HeightPx)
	decoded, err := png.Decode(bytes.NewReader(out.PNG))
	if err != nil {
		t.Fatal(err)
	}
	leftR, _, leftB, _ := decoded.At(0, 0).RGBA()
	rightR, _, rightB, _ := decoded.At(spec.WidthPx-1, spec.HeightPx-1).RGBA()
	if leftR <= leftB {
		t.Fatalf("left pixel lost the red side: r=%d b=%d", leftR, leftB)
	}
	if rightB <= rightR {
		t.Fatalf("right pixel lost the blue side (crop or pad): r=%d b=%d", rightR, rightB)
	}
}

func TestPrepare_ChecksumStable(t *testing.T) {
	t.Parallel()
	spec := testSpec()
	raw := mustPNG(t, spec.WidthPx, spec.HeightPx, false)
	a, err := Prepare(raw, spec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Prepare(raw, spec)
	if err != nil {
		t.Fatal(err)
	}
	if a.Checksum != b.Checksum || !bytes.Equal(a.PNG, b.PNG) {
		t.Fatalf("checksum %s vs %s", a.Checksum, b.Checksum)
	}
	sum := sha256.Sum256(a.PNG)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if a.Checksum != want {
		t.Fatalf("checksum = %s, want %s", a.Checksum, want)
	}
	if _, err := png.Decode(bytes.NewReader(a.PNG)); err != nil {
		t.Fatal(err)
	}
}

func TestPrepare_InvalidSpec(t *testing.T) {
	t.Parallel()
	png := mustPNG(t, 8, 8, false)
	cases := []Spec{
		{},
		{WidthPx: 8, HeightPx: 8, DPI: 72, ColorProfile: "cmyk", UpscalePolicy: PolicyReject},
		{WidthPx: 8, HeightPx: 8, DPI: 72, ColorProfile: ProfileSRGB, UpscalePolicy: "pad"},
	}
	for _, spec := range cases {
		_, err := Prepare(png, spec)
		var se *SpecError
		var pe *Error
		if !errors.As(err, &se) || errors.As(err, &pe) {
			t.Fatalf("spec %+v err=%v", spec, err)
		}
		if err := ValidateSpec(spec); !errors.As(err, &se) {
			t.Fatalf("ValidateSpec %+v err=%v", spec, err)
		}
	}
	if err := ValidateSpec(specSize(8, 8)); err != nil {
		t.Fatal(err)
	}
}

func TestPrepare_MetadataDPI(t *testing.T) {
	t.Parallel()
	spec := specSize(12, 12)
	spec.DPI = 300
	out, err := Prepare(mustPNG(t, 12, 12, false), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertMeta(t, out.PNG, 300)
	ppm := readPHYS(t, out.PNG)
	if ppm != uint32(mathRound(300/0.0254)) {
		t.Fatalf("ppm = %d", ppm)
	}
}

func testSpec() Spec {
	return specSize(20, 20)
}

func specSize(w, h int) Spec {
	return Spec{
		WidthPx: w, HeightPx: h, DPI: 72,
		ColorProfile: ProfileSRGB, UpscalePolicy: PolicyReject,
	}
}

func specTransparency(s Spec) Spec {
	s.RequireTransparency = true
	return s
}

func mustPNG(t *testing.T, w, h int, alpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	c := color.NRGBA{R: 200, G: 10, B: 10, A: 255}
	if alpha {
		c.A = 128
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return encodePNG(t, img)
}

func mustPalette(t *testing.T, w, h int, transparent bool) []byte {
	t.Helper()
	pal := color.Palette{
		color.NRGBA{R: 255, G: 0, B: 0, A: 255},
		color.NRGBA{R: 0, G: 0, B: 0, A: 255},
	}
	if transparent {
		pal[1] = color.NRGBA{R: 0, G: 0, B: 0, A: 0}
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	if transparent {
		img.SetColorIndex(0, 0, 1)
	}
	return encodePNG(t, img)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertSize(t *testing.T, raw []byte, w, h int) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != w || cfg.Height != h {
		t.Fatalf("size = %dx%d, want %dx%d", cfg.Width, cfg.Height, w, h)
	}
}

func assertMeta(t *testing.T, raw []byte, dpi int) {
	t.Helper()
	var srgb []byte
	var phys []byte
	err := walkPNGChunks(raw, func(typ string, data []byte) error {
		switch typ {
		case "sRGB":
			srgb = append([]byte(nil), data...)
		case "pHYs":
			phys = append([]byte(nil), data...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(srgb) != 1 || srgb[0] != 0 {
		t.Fatalf("sRGB chunk = %v", srgb)
	}
	if len(phys) != 9 || phys[8] != 1 {
		t.Fatalf("pHYs = %v", phys)
	}
	x := binary.BigEndian.Uint32(phys[0:4])
	y := binary.BigEndian.Uint32(phys[4:8])
	want := uint32(mathRound(float64(dpi) / 0.0254))
	if x != want || y != want {
		t.Fatalf("pHYs ppm = %d,%d want %d", x, y, want)
	}
}

func readPHYS(t *testing.T, raw []byte) uint32 {
	t.Helper()
	var ppm uint32
	err := walkPNGChunks(raw, func(typ string, data []byte) error {
		if typ == "pHYs" && len(data) >= 4 {
			ppm = binary.BigEndian.Uint32(data[0:4])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ppm
}

func pngAlpha(t *testing.T, raw []byte) bool {
	t.Helper()
	ok, err := pngHasAlpha(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func mathRound(v float64) float64 {
	if v < 0 {
		return float64(int64(v - 0.5))
	}
	return float64(int64(v + 0.5))
}
