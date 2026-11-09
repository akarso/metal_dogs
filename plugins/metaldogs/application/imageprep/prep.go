// Package imageprep prepares customer uploads: bounded decode, orientation normalize, EXIF strip.
package imageprep

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"

	"github.com/akarso/shopanda/plugins/metaldogs/generation"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/imagemime"
)

// Dimension bounds for customer source photos (C2).
const (
	MinDimension = 256
	MaxDimension = generation.MaxDimension // 2048
	MaxPixels    = MaxDimension * MaxDimension
	JPEGQuality  = 90
)

// Prepared is a re-encoded upright image ready for private storage.
type Prepared struct {
	Bytes    []byte
	MimeType string
	Ext      string // jpg | png (no leading dot)
	Width    int
	Height   int
}

// ErrCode classifies prepare failures for HTTP mapping.
type ErrCode string

const (
	ErrUnsupportedMedia ErrCode = "unsupported_media_type"
	ErrInvalidImage     ErrCode = "invalid_image"
	ErrDimensions       ErrCode = "dimensions_out_of_range"
)

// Error is a classified prepare failure.
type Error struct {
	Code    ErrCode
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// Prepare sniffs (JPEG/PNG only), gates on DecodeConfig, applies EXIF Orientation,
// then re-encodes to strip metadata. WebP is rejected until a re-encode path exists.
func Prepare(raw []byte) (Prepared, error) {
	mime := imagemime.Sniff(raw)
	switch mime {
	case imagemime.JPEG, imagemime.PNG:
	case imagemime.WEBP:
		return Prepared{}, &Error{Code: ErrUnsupportedMedia, Message: "webp not accepted for uploads"}
	default:
		return Prepared{}, &Error{Code: ErrUnsupportedMedia, Message: "unsupported image type"}
	}

	cfg, err := decodeConfig(mime, raw)
	if err != nil {
		return Prepared{}, &Error{Code: ErrInvalidImage, Message: "image config decode failed"}
	}
	if err := checkConfigBounds(cfg.Width, cfg.Height); err != nil {
		return Prepared{}, err
	}

	orient := 1
	if mime == imagemime.JPEG {
		orient = jpegOrientation(raw)
	}

	img, err := decodeImage(mime, raw)
	if err != nil {
		return Prepared{}, &Error{Code: ErrInvalidImage, Message: "image decode failed"}
	}
	img = applyOrientation(img, orient)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if err := checkUprightBounds(w, h); err != nil {
		return Prepared{}, err
	}

	var out bytes.Buffer
	var ext string
	switch mime {
	case imagemime.JPEG:
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: JPEGQuality}); err != nil {
			return Prepared{}, fmt.Errorf("imageprep: jpeg encode: %w", err)
		}
		ext = "jpg"
	case imagemime.PNG:
		if err := png.Encode(&out, img); err != nil {
			return Prepared{}, fmt.Errorf("imageprep: png encode: %w", err)
		}
		ext = "png"
	}
	return Prepared{
		Bytes:    out.Bytes(),
		MimeType: mime,
		Ext:      ext,
		Width:    w,
		Height:   h,
	}, nil
}

func decodeConfig(mime string, raw []byte) (image.Config, error) {
	r := bytes.NewReader(raw)
	switch mime {
	case imagemime.JPEG:
		return jpeg.DecodeConfig(r)
	case imagemime.PNG:
		return png.DecodeConfig(r)
	default:
		return image.Config{}, fmt.Errorf("unsupported")
	}
}

func decodeImage(mime string, raw []byte) (image.Image, error) {
	r := bytes.NewReader(raw)
	switch mime {
	case imagemime.JPEG:
		return jpeg.Decode(r)
	case imagemime.PNG:
		return png.Decode(r)
	default:
		return nil, fmt.Errorf("unsupported")
	}
}

// checkConfigBounds is the OOM gate before full decode (edges + pixel count).
func checkConfigBounds(w, h int) error {
	if w <= 0 || h <= 0 {
		return &Error{Code: ErrInvalidImage, Message: "invalid dimensions"}
	}
	if w > MaxDimension || h > MaxDimension {
		return &Error{
			Code:    ErrDimensions,
			Message: fmt.Sprintf("dimensions %dx%d out of range [%d,%d]", w, h, MinDimension, MaxDimension),
		}
	}
	if w > 0 && h > MaxPixels/w { // avoid w*h overflow
		return &Error{
			Code:    ErrDimensions,
			Message: fmt.Sprintf("pixel count exceeds max %d", MaxPixels),
		}
	}
	if w*h > MaxPixels {
		return &Error{
			Code:    ErrDimensions,
			Message: fmt.Sprintf("pixel count %d exceeds max %d", w*h, MaxPixels),
		}
	}
	return nil
}

func checkUprightBounds(w, h int) error {
	if w < MinDimension || h < MinDimension || w > MaxDimension || h > MaxDimension {
		return &Error{
			Code:    ErrDimensions,
			Message: fmt.Sprintf("dimensions %dx%d out of range [%d,%d]", w, h, MinDimension, MaxDimension),
		}
	}
	return nil
}

// applyOrientation maps EXIF orientation 1–8 into an upright pixel buffer.
// Materializes to RGBA once so jpeg.YCbCr does not pay At() conversion per pixel.
func applyOrientation(src image.Image, orient int) image.Image {
	if orient < 2 || orient > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	flat := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(flat, flat.Bounds(), src, b.Min, draw.Src)

	var dst *image.RGBA
	switch orient {
	case 2, 3, 4:
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	case 5, 6, 7, 8:
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	default:
		return flat
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*flat.Stride + x*4
			c := color.RGBA{R: flat.Pix[i], G: flat.Pix[i+1], B: flat.Pix[i+2], A: flat.Pix[i+3]}
			switch orient {
			case 2:
				dst.SetRGBA(w-1-x, y, c)
			case 3:
				dst.SetRGBA(w-1-x, h-1-y, c)
			case 4:
				dst.SetRGBA(x, h-1-y, c)
			case 5:
				dst.SetRGBA(y, x, c)
			case 6:
				dst.SetRGBA(h-1-y, x, c)
			case 7:
				dst.SetRGBA(h-1-y, w-1-x, c)
			case 8:
				dst.SetRGBA(y, w-1-x, c)
			}
		}
	}
	return dst
}
