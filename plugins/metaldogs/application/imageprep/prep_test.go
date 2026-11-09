package imageprep_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
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

func TestPrepare_OversizeIHDR_BeforeFullDecode(t *testing.T) {
	t.Parallel()
	raw := rgbPNGWithDims(generation.MaxDimension+1, 1, 10, 20, 30)
	_, err := imageprep.Prepare(raw)
	var pe *imageprep.Error
	if err == nil || !asPrepErr(err, &pe) || pe.Code != imageprep.ErrDimensions {
		t.Fatalf("oversize IHDR must fail on DecodeConfig gate, err=%v", err)
	}
}

func TestPrepare_EXIFStripped(t *testing.T) {
	t.Parallel()
	base := encodeJPEG(t, solidRGBA(280, 280))
	withEXIF := injectExifOrientation(base, 1)
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

func TestPrepare_Orientation6_Upright(t *testing.T) {
	t.Parallel()
	// Landscape 320×280 with a red block at top-left. Orientation=6 → upright 280×320
	// with that block at top-right (lossy JPEG: sample a region average).
	src := solidRGBA(320, 280)
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			src.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	raw := injectExifOrientation(encodeJPEG(t, src), 6)
	got, err := imageprep.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 280 || got.Height != 320 {
		t.Fatalf("upright dims = %dx%d, want 280x320", got.Width, got.Height)
	}
	out, err := jpeg.Decode(bytes.NewReader(got.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if avgRed(out, 250, 0, 279, 20) < 180 {
		t.Fatalf("expected red block at upright top-right after Orientation=6")
	}
	if avgRed(out, 0, 0, 20, 20) > 80 {
		t.Fatalf("top-left should not stay red after Orientation=6 normalize")
	}
	if bytes.Contains(got.Bytes, []byte("Exif")) {
		t.Fatal("orientation EXIF must be stripped after normalize")
	}
}

func avgRed(img image.Image, x0, y0, x1, y1 int) int {
	var sum, n int
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			sum += int(r >> 8)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

func injectExifOrientation(jpegBytes []byte, orient int) []byte {
	// Minimal little-endian Exif APP1 with Orientation tag.
	tiff := []byte{
		'I', 'I',
		0x2a, 0x00,
		0x08, 0x00, 0x00, 0x00, // IFD0 at 8
		0x01, 0x00, // 1 entry
		0x12, 0x01, // tag Orientation
		0x03, 0x00, // SHORT
		0x01, 0x00, 0x00, 0x00,
		byte(orient), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, // next IFD
	}
	exif := append([]byte{'E', 'x', 'i', 'f', 0, 0}, tiff...)
	seglen := len(exif) + 2
	app1 := make([]byte, 0, 4+len(exif))
	app1 = append(app1, 0xff, 0xe1, byte(seglen>>8), byte(seglen))
	app1 = append(app1, exif...)
	out := make([]byte, 0, len(jpegBytes)+len(app1))
	out = append(out, jpegBytes[:2]...)
	out = append(out, app1...)
	out = append(out, jpegBytes[2:]...)
	return out
}

// rgbPNGWithDims builds a 1-pixel-row colour-type-2 PNG with arbitrary IHDR size
// (IDAT still encodes 1×1) so DecodeConfig can reject oversize bombs.
func rgbPNGWithDims(w, h int, r, g, b byte) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	writePNGChunk(&out, "IHDR", func(p *bytes.Buffer) {
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
	writePNGChunk(&out, "IDAT", func(p *bytes.Buffer) { p.Write(zbuf.Bytes()) })
	writePNGChunk(&out, "IEND", func(p *bytes.Buffer) {})
	return out.Bytes()
}

func writePNGChunk(out *bytes.Buffer, typ string, body func(*bytes.Buffer)) {
	var payload bytes.Buffer
	payload.WriteString(typ)
	body(&payload)
	_ = binary.Write(out, binary.BigEndian, uint32(payload.Len()-4))
	out.Write(payload.Bytes())
	crc := crc32.ChecksumIEEE(payload.Bytes())
	_ = binary.Write(out, binary.BigEndian, crc)
}

func asPrepErr(err error, target **imageprep.Error) bool {
	if pe, ok := err.(*imageprep.Error); ok {
		*target = pe
		return true
	}
	return false
}
