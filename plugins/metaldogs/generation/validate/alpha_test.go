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

	"github.com/akarso/shopanda/plugins/metaldogs/generation/validate"
)

func TestHasUsefulAlpha_OpaqueRGBA(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if validate.HasUsefulAlpha(buf.Bytes()) {
		t.Fatal("opaque RGBA must not report HasUsefulAlpha")
	}
}

func TestHasUsefulAlpha_PartialRGBA(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 1, G: 2, B: 3, A: 128})
	img.SetRGBA(0, 1, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetRGBA(1, 1, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if !validate.HasUsefulAlpha(buf.Bytes()) {
		t.Fatal("partial alpha RGBA must report HasUsefulAlpha")
	}
}

func TestHasUsefulAlpha_RGBwithTRNS(t *testing.T) {
	t.Parallel()
	raw := rgbPNGWithTRNS(10, 20, 30)
	if !validate.HasUsefulAlpha(raw) {
		t.Fatal("RGB+tRNS must report HasUsefulAlpha after decode")
	}
}

func TestHasUsefulAlpha_Corrupt(t *testing.T) {
	t.Parallel()
	if validate.HasUsefulAlpha([]byte("nope")) {
		t.Fatal("corrupt PNG must be false")
	}
}

// rgbPNGWithTRNS builds a 1×1 colour-type-2 PNG whose sole pixel matches tRNS.
func rgbPNGWithTRNS(r, g, b byte) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	writePNGChunk(&out, "IHDR", func(p *bytes.Buffer) {
		_ = binary.Write(p, binary.BigEndian, uint32(1))
		_ = binary.Write(p, binary.BigEndian, uint32(1))
		p.WriteByte(8) // bit depth
		p.WriteByte(2) // colour type RGB
		p.WriteByte(0) // compression
		p.WriteByte(0) // filter
		p.WriteByte(0) // interlace
	})
	writePNGChunk(&out, "tRNS", func(p *bytes.Buffer) {
		_ = binary.Write(p, binary.BigEndian, uint16(r))
		_ = binary.Write(p, binary.BigEndian, uint16(g))
		_ = binary.Write(p, binary.BigEndian, uint16(b))
	})
	var raw bytes.Buffer
	raw.WriteByte(0) // filter none
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
