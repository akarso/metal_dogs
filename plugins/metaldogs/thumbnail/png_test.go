package thumbnail_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

func TestPNG_LongestEdgeAndNoAncillaryChunks(t *testing.T) {
	t.Parallel()
	wide := mustPNG(t, 800, 200)
	wide = insertTextChunk(t, wide, "Comment", "EXIF-SECRET")
	out, w, h, err := thumbnail.PNG(wide, thumbnail.MaxEdge)
	if err != nil {
		t.Fatal(err)
	}
	if w != thumbnail.MaxEdge || h != 80 || w > thumbnail.MaxEdge || h > thumbnail.MaxEdge {
		t.Fatalf("size %dx%d", w, h)
	}
	if bytes.Contains(out, []byte("EXIF-SECRET")) {
		t.Fatal("source text survived")
	}
	assertCriticalChunks(t, out)

	small := mustPNG(t, 100, 40)
	out, w, h, err = thumbnail.PNG(small, thumbnail.MaxEdge)
	if err != nil {
		t.Fatal(err)
	}
	if w != 100 || h != 40 {
		t.Fatalf("upscaled %dx%d", w, h)
	}
	assertCriticalChunks(t, out)
}

func TestPNG_RejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, _, _, err := thumbnail.PNG([]byte("not-an-image"), thumbnail.MaxEdge); err == nil {
		t.Fatal("garbage decoded")
	}
	if _, _, _, err := thumbnail.PNG(mustPNG(t, 2, 2), 0); err == nil {
		t.Fatal("zero edge accepted")
	}
}

func mustPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func insertTextChunk(t *testing.T, raw []byte, key, val string) []byte {
	t.Helper()
	iend := []byte("IEND")
	at := bytes.LastIndex(raw, iend)
	if at < 4 {
		t.Fatal("no IEND")
	}
	at -= 4
	payload := append(append([]byte(key), 0), []byte(val)...)
	chunk := make([]byte, 4+4+len(payload)+4)
	binary.BigEndian.PutUint32(chunk[0:4], uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], payload)
	sum := crc32.ChecksumIEEE(chunk[4 : 8+len(payload)])
	binary.BigEndian.PutUint32(chunk[8+len(payload):], sum)
	out := append([]byte{}, raw[:at]...)
	out = append(out, chunk...)
	out = append(out, raw[at:]...)
	return out
}

func assertCriticalChunks(t *testing.T, raw []byte) {
	t.Helper()
	if len(raw) < 8 || string(raw[1:4]) != "PNG" {
		t.Fatal("not a png")
	}
	rest := raw[8:]
	for len(rest) > 0 {
		if len(rest) < 12 {
			t.Fatal("truncated chunk")
		}
		n := binary.BigEndian.Uint32(rest[:4])
		kind := string(rest[4:8])
		switch kind {
		case "IHDR", "IDAT", "IEND":
		default:
			t.Fatalf("ancillary chunk %s", kind)
		}
		rest = rest[12+int(n):]
	}
}
