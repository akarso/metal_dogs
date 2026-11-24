// Package production fits an approved artwork PNG to a print spec.
package production

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"image/png"
	"math"

	"github.com/disintegration/imaging"
)

const (
	// ProfileSRGB is the only accepted color profile.
	ProfileSRGB = "srgb"
	// PolicyReject fails when the artwork is smaller than the spec.
	PolicyReject = "reject"
	// PolicyUpscale scales a smaller artwork up to the spec.
	PolicyUpscale = "upscale"

	// CodeAspectMismatch means the artwork aspect differs from the spec by more than 1%.
	CodeAspectMismatch = "aspect_mismatch"
	// CodeTooSmall means the artwork is smaller than the spec and policy is reject.
	CodeTooSmall = "too_small"
	// CodeTransparencyRequired means the spec requires an alpha channel and the PNG has none.
	CodeTransparencyRequired = "transparency_required"
	// CodeArtworkUnreadable means the bytes are not a decodable PNG.
	CodeArtworkUnreadable = "artwork_unreadable"

	checksumPrefix = "sha256:"
)

// Spec is the print size and policy for one production file.
type Spec struct {
	WidthPx             int
	HeightPx            int
	DPI                 int
	RequireTransparency bool
	ColorProfile        string
	UpscalePolicy       string
}

// Output is the print-ready PNG and its sha256 checksum.
type Output struct {
	PNG      []byte
	Checksum string
}

// Error is a permanent artwork failure. Code is stable.
type Error struct {
	Code string
}

// SpecError is a rejected print spec. It is not an artwork failure.
type SpecError struct {
	Reason string
}

func (e *SpecError) Error() string {
	if e == nil || e.Reason == "" {
		return "production: invalid spec"
	}
	return "production: invalid spec: " + e.Reason
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// Prepare fits artwork PNG to spec and returns the print PNG plus a sha256 checksum.
// Failures are *Error with a stable Code. The output is the exact spec size (no crop, no padding) and carries pHYs and sRGB.
func Prepare(artwork []byte, spec Spec) (Output, error) {
	if err := ValidateSpec(spec); err != nil {
		return Output{}, err
	}
	img, err := png.Decode(bytes.NewReader(artwork))
	if err != nil {
		return Output{}, &Error{Code: CodeArtworkUnreadable}
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return Output{}, &Error{Code: CodeArtworkUnreadable}
	}
	if !aspectWithinOnePercent(srcW, srcH, spec.WidthPx, spec.HeightPx) {
		return Output{}, &Error{Code: CodeAspectMismatch}
	}
	if spec.RequireTransparency {
		hasAlpha, alphaErr := pngHasAlpha(artwork)
		if alphaErr != nil {
			return Output{}, &Error{Code: CodeArtworkUnreadable}
		}
		if !hasAlpha {
			return Output{}, &Error{Code: CodeTransparencyRequired}
		}
	}
	if srcW < spec.WidthPx || srcH < spec.HeightPx {
		if spec.UpscalePolicy == PolicyReject {
			return Output{}, &Error{Code: CodeTooSmall}
		}
	}
	fitted := imaging.Resize(img, spec.WidthPx, spec.HeightPx, imaging.Lanczos)
	var buf bytes.Buffer
	if err := png.Encode(&buf, fitted); err != nil {
		return Output{}, fmt.Errorf("production: encode png: %w", err)
	}
	out, err := withPrintMeta(buf.Bytes(), spec.DPI)
	if err != nil {
		return Output{}, fmt.Errorf("production: png metadata: %w", err)
	}
	sum := sha256.Sum256(out)
	return Output{
		PNG:      out,
		Checksum: checksumPrefix + hex.EncodeToString(sum[:]),
	}, nil
}

// ValidateSpec rejects a spec the fitter cannot apply. Settings uses the same rules at Init.
func ValidateSpec(spec Spec) error {
	if spec.WidthPx <= 0 || spec.HeightPx <= 0 || spec.DPI <= 0 {
		return &SpecError{Reason: "width_px, height_px, and dpi must be > 0"}
	}
	if spec.ColorProfile != ProfileSRGB {
		return &SpecError{Reason: "color_profile is not supported"}
	}
	switch spec.UpscalePolicy {
	case PolicyReject, PolicyUpscale:
	default:
		return &SpecError{Reason: "upscale_policy is not supported"}
	}
	return nil
}

func aspectWithinOnePercent(srcW, srcH, dstW, dstH int) bool {
	// |srcW/srcH - dstW/dstH| / (dstW/dstH) <= 0.01
	diff := abs64(int64(srcW)*int64(dstH)-int64(dstW)*int64(srcH)) * 100
	limit := int64(dstW) * int64(srcH)
	return diff <= limit
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func dpiToPPM(dpi int) uint32 {
	return uint32(math.Round(float64(dpi) / 0.0254))
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func pngHasAlpha(raw []byte) (bool, error) {
	var colorType byte
	var sawIHDR bool
	var trns bool
	err := walkPNGChunks(raw, func(typ string, data []byte) error {
		switch typ {
		case "IHDR":
			if len(data) < 13 {
				return fmt.Errorf("short IHDR")
			}
			colorType = data[9]
			sawIHDR = true
		case "tRNS":
			trns = true
		}
		return nil
	})
	if err != nil || !sawIHDR {
		return false, fmt.Errorf("production: png chunks")
	}
	// 4 = gray+alpha, 6 = truecolor+alpha. tRNS adds transparency to other types.
	return colorType == 4 || colorType == 6 || trns, nil
}

func withPrintMeta(raw []byte, dpi int) ([]byte, error) {
	ihdrEnd := 0
	err := walkPNGChunks(raw, func(typ string, data []byte) error {
		if typ == "IHDR" {
			ihdrEnd = 8 + 12 + len(data)
			return errStopWalk
		}
		return fmt.Errorf("IHDR missing")
	})
	if err != nil && err != errStopWalk {
		return nil, err
	}
	if ihdrEnd <= 0 || ihdrEnd > len(raw) {
		return nil, fmt.Errorf("IHDR missing")
	}
	ppm := dpiToPPM(dpi)
	phys := make([]byte, 9)
	binary.BigEndian.PutUint32(phys[0:4], ppm)
	binary.BigEndian.PutUint32(phys[4:8], ppm)
	phys[8] = 1
	extra := append(pngChunk("sRGB", []byte{0}), pngChunk("pHYs", phys)...)
	out := make([]byte, 0, len(raw)+len(extra))
	out = append(out, raw[:ihdrEnd]...)
	out = append(out, extra...)
	out = append(out, raw[ihdrEnd:]...)
	return out, nil
}

var errStopWalk = fmt.Errorf("stop")

func walkPNGChunks(raw []byte, fn func(typ string, data []byte) error) error {
	if len(raw) < len(pngSignature) || !bytes.Equal(raw[:len(pngSignature)], pngSignature) {
		return fmt.Errorf("not a png")
	}
	i := len(pngSignature)
	for i+12 <= len(raw) {
		n := int(binary.BigEndian.Uint32(raw[i : i+4]))
		if n < 0 || i+12+n > len(raw) {
			return fmt.Errorf("truncated chunk")
		}
		typ := string(raw[i+4 : i+8])
		data := raw[i+8 : i+8+n]
		if err := fn(typ, data); err != nil {
			return err
		}
		i += 12 + n
		if typ == "IEND" {
			return nil
		}
	}
	return fmt.Errorf("truncated png")
}

func pngChunk(typ string, data []byte) []byte {
	buf := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:8], typ)
	copy(buf[8:8+len(data)], data)
	sum := crc32.ChecksumIEEE(buf[4 : 8+len(data)])
	binary.BigEndian.PutUint32(buf[8+len(data):], sum)
	return buf
}
