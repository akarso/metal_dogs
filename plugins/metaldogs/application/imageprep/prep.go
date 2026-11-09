// Package imageprep prepares customer uploads: decode bounds and EXIF-stripping re-encode.
package imageprep

import (
	"bytes"
	"fmt"
	"image"
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
	JPEGQuality  = 90
)

// Prepared is a re-encoded image ready for private storage.
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

// Prepare sniffs (JPEG/PNG only), decodes, checks bounds, and re-encodes to strip metadata.
// WebP is rejected until a re-encode path exists without new heavy deps.
func Prepare(raw []byte) (Prepared, error) {
	mime := imagemime.Sniff(raw)
	switch mime {
	case imagemime.JPEG, imagemime.PNG:
		// ok
	case imagemime.WEBP:
		return Prepared{}, &Error{Code: ErrUnsupportedMedia, Message: "webp not accepted for uploads"}
	default:
		return Prepared{}, &Error{Code: ErrUnsupportedMedia, Message: "unsupported image type"}
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Prepared{}, &Error{Code: ErrInvalidImage, Message: "image decode failed"}
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < MinDimension || h < MinDimension || w > MaxDimension || h > MaxDimension {
		return Prepared{}, &Error{
			Code:    ErrDimensions,
			Message: fmt.Sprintf("dimensions %dx%d out of range [%d,%d]", w, h, MinDimension, MaxDimension),
		}
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
