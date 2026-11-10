package imagemime_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation/imagemime"
)

func TestSniff_JPEGPNGWebPGarbage(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	webp := []byte("RIFF....WEBP....") // minimal RIFF/WEBP magic
	copy(webp[4:8], []byte{0, 0, 0, 0})

	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"jpeg", jpegBuf.Bytes(), imagemime.JPEG},
		{"png", pngBuf.Bytes(), imagemime.PNG},
		{"webp", webp, imagemime.WEBP},
		{"garbage", []byte("not-an-image"), ""},
		{"empty", nil, ""},
		{"mislabeled", append([]byte("fake.jpg"), 0, 1, 2), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := imagemime.Sniff(tc.in); got != tc.want {
				t.Fatalf("Sniff = %q, want %q", got, tc.want)
			}
		})
	}
}
