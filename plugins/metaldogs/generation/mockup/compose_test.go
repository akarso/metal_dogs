package mockup_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/mockup"
)

func TestCompose_PlacesArtworkInPrintArea(t *testing.T) {
	t.Parallel()
	art := solidPNG(t, 32, 32, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	out, err := mockup.Compose(art, mockup.DefaultBasePNG(), mockup.DefaultPrintArea())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 80 || b.Dy() != 100 {
		t.Fatalf("size=%dx%d", b.Dx(), b.Dy())
	}
	// Center of print area should be near white from artwork.
	cx := mockup.DefaultPrintArea().X + mockup.DefaultPrintArea().Width/2
	cy := mockup.DefaultPrintArea().Y + mockup.DefaultPrintArea().Height/2
	r, g, bl, a := img.At(cx, cy).RGBA()
	if a>>8 < 200 || r>>8 < 200 || g>>8 < 200 || bl>>8 < 200 {
		t.Fatalf("center pixel not light: rgba=%d,%d,%d,%d", r>>8, g>>8, bl>>8, a>>8)
	}
	// Corner outside print area stays dark.
	r, g, bl, a = img.At(2, 2).RGBA()
	if r>>8 > 60 || g>>8 > 60 || bl>>8 > 60 {
		t.Fatalf("garment corner not dark: rgba=%d,%d,%d,%d", r>>8, g>>8, bl>>8, a>>8)
	}
}

func TestCompose_LetterboxTransparentPNG(t *testing.T) {
	t.Parallel()
	// Wide transparent canvas with an opaque red stripe; contain-scale into 40×40 must letterbox.
	art := image.NewRGBA(image.Rect(0, 0, 40, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			if x >= 10 && x < 30 {
				art.SetRGBA(x, y, color.RGBA{R: 220, A: 255})
			} else {
				art.SetRGBA(x, y, color.RGBA{A: 0})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, art); err != nil {
		t.Fatal(err)
	}
	place := mockup.DefaultPrintArea()
	out, err := mockup.Compose(buf.Bytes(), mockup.DefaultBasePNG(), place)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	cx := place.X + place.Width/2
	cy := place.Y + place.Height/2
	r, g, bl, a := img.At(cx, cy).RGBA()
	if a>>8 < 200 || r>>8 < 150 || g>>8 > 80 || bl>>8 > 80 {
		t.Fatalf("center should show red over tee: rgba=%d,%d,%d,%d", r>>8, g>>8, bl>>8, a>>8)
	}
	// Top of print area (letterbox band) should stay near black from the tee, not stretched red.
	topY := place.Y + 2
	r, g, bl, a = img.At(cx, topY).RGBA()
	if r>>8 > 80 {
		t.Fatalf("letterbox band must not stretch artwork: rgba=%d,%d,%d,%d", r>>8, g>>8, bl>>8, a>>8)
	}
}

func TestCompose_RejectsOutOfBounds(t *testing.T) {
	t.Parallel()
	art := solidPNG(t, 8, 8, color.RGBA{A: 255})
	_, err := mockup.Compose(art, mockup.DefaultBasePNG(), domain.PrintArea{X: 70, Y: 90, Width: 40, Height: 40})
	if err == nil {
		t.Fatal("want bounds error")
	}
}

func TestCompose_RejectsEmptyArtwork(t *testing.T) {
	t.Parallel()
	_, err := mockup.Compose(nil, mockup.DefaultBasePNG(), mockup.DefaultPrintArea())
	if err == nil {
		t.Fatal("want error")
	}
}

func TestCompose_RejectsZeroPrintArea(t *testing.T) {
	t.Parallel()
	art := solidPNG(t, 4, 4, color.RGBA{A: 255})
	_, err := mockup.Compose(art, mockup.DefaultBasePNG(), domain.PrintArea{X: 0, Y: 0, Width: 0, Height: 10})
	if err == nil {
		t.Fatal("want error")
	}
}

func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
