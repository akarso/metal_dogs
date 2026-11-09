package imageprep_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/application/imageprep"
	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/imagemime"
)

func solidRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 80, B: 120, A: 255})
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrepare_HappyJPEGAndPNG(t *testing.T) {
	t.Parallel()
	img := solidRGBA(300, 300)
	got, err := imageprep.Prepare(encodeJPEG(t, img))
	if err != nil || got.MimeType != imagemime.JPEG || got.Ext != "jpg" || got.Width != 300 || got.Height != 300 {
		t.Fatalf("jpeg: %+v err=%v", got, err)
	}
	got, err = imageprep.Prepare(encodePNG(t, img))
	if err != nil || got.MimeType != imagemime.PNG || got.Ext != "png" {
		t.Fatalf("png: %+v err=%v", got, err)
	}
}

func TestPrepare_WebPUnsupported(t *testing.T) {
	t.Parallel()
	webp := make([]byte, 12)
	copy(webp[0:4], "RIFF")
	copy(webp[8:12], "WEBP")
	_, err := imageprep.Prepare(webp)
	var pe *imageprep.Error
	if err == nil || !asPrepErr(err, &pe) || pe.Code != imageprep.ErrUnsupportedMedia {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepare_GarbageUnsupported(t *testing.T) {
	t.Parallel()
	_, err := imageprep.Prepare([]byte("not-an-image.jpg"))
	var pe *imageprep.Error
	if err == nil || !asPrepErr(err, &pe) || pe.Code != imageprep.ErrUnsupportedMedia {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepare_CorruptDecode(t *testing.T) {
	t.Parallel()
	// Valid JPEG SOI + truncated body → sniff ok, decode fails.
	raw := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 1}
	_, err := imageprep.Prepare(raw)
	var pe *imageprep.Error
	if err == nil || !asPrepErr(err, &pe) || pe.Code != imageprep.ErrInvalidImage {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepare_Dimensions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		w, h int
		ok   bool
	}{
		{"min", imageprep.MinDimension, imageprep.MinDimension, true},
		{"below_min_w", imageprep.MinDimension - 1, 300, false},
		{"below_min_h", 300, imageprep.MinDimension - 1, false},
		{"above_max", generation.MaxDimension + 1, 300, false},
		{"max", generation.MaxDimension, generation.MaxDimension, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := imageprep.Prepare(encodeJPEG(t, solidRGBA(tc.w, tc.h)))
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var pe *imageprep.Error
			if err == nil || !asPrepErr(err, &pe) || pe.Code != imageprep.ErrDimensions {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestPrepare_EXIFStripped(t *testing.T) {
	t.Parallel()
	base := encodeJPEG(t, solidRGBA(280, 280))
	withEXIF := injectFakeEXIF(base)
	if !bytes.Contains(withEXIF, []byte("Exif")) {
		t.Fatal("fixture must contain Exif marker")
	}
	inSum := sha256.Sum256(withEXIF)
	got, err := imageprep.Prepare(withEXIF)
	if err != nil {
		t.Fatal(err)
	}
	outSum := sha256.Sum256(got.Bytes)
	if hex.EncodeToString(inSum[:]) == hex.EncodeToString(outSum[:]) {
		t.Fatal("re-encoded bytes must differ from EXIF input")
	}
	if bytes.Contains(got.Bytes, []byte("Exif")) {
		t.Fatal("re-encoded JPEG must not retain Exif APP1")
	}
}

func injectFakeEXIF(jpegBytes []byte) []byte {
	// Insert APP1 after SOI (ff d8). Payload: length + "Exif\0\0" + pad.
	app1Payload := []byte{
		0xff, 0xe1, 0x00, 0x10,
		'E', 'x', 'i', 'f', 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	out := make([]byte, 0, len(jpegBytes)+len(app1Payload))
	out = append(out, jpegBytes[:2]...) // SOI
	out = append(out, app1Payload...)
	out = append(out, jpegBytes[2:]...)
	return out
}

func asPrepErr(err error, target **imageprep.Error) bool {
	if pe, ok := err.(*imageprep.Error); ok {
		*target = pe
		return true
	}
	return false
}
