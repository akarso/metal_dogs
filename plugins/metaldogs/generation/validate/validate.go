// Package validate implements G3 deterministic checks on provider PNG output.
package validate

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// Status values for Result.
const (
	StatusPass = "pass"
	StatusFail = "fail"
)

// Validation code constants (docs/architecture/flows/generation-pipeline.md).
const (
	CodeDecodeFailed       = "decode_failed"
	CodeDimensionsMismatch = "dimensions_mismatch"
	CodeNotMonochrome      = "not_monochrome"
	CodeNoTransparency     = "no_transparency"
	CodeBlankOrLowDetail   = "blank_or_low_detail"
)

// MonochromeTolerance is the max (maxRGB−minRGB) on 8-bit non-premultiplied
// channels for monochrome pass.
const MonochromeTolerance = 16

// AlphaChromaFloor is the minimum 8-bit alpha for a pixel to contribute to
// blank detection (near-transparent leftover RGB under cutouts is ignored).
//
// Monochrome intentionally uses a stricter gate (any a16 > 0, un-premultiplied)
// so low-alpha chromatic fringe cannot hide colour. Before C4 job wiring,
// decide whether live provider fringe false-fails warrant aligning mono to this
// floor (tunable); document the choice in generation-pipeline.md.
const AlphaChromaFloor = 16

// BlankMaxUniqueColors: after 4-bit-per-channel quantize, ≤ this many colors is
// a blank candidate (also requires low luminance variance — see BlankVarianceFloor).
const BlankMaxUniqueColors = 4

// BlankVarianceFloor is the minimum population luminance variance (8-bit Y) required
// to pass when unique quantized colors ≤ BlankMaxUniqueColors.
const BlankVarianceFloor = 80.0

// Result is the outcome of Validate.
// Codes holds hard-fail codes only; Warnings holds soft codes (e.g. no_transparency).
type Result struct {
	Status   string   `json:"status"` // pass | fail
	Codes    []string `json:"codes,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Message  string   `json:"message,omitempty"`
}

// Failed reports hard validation failure.
func (r Result) Failed() bool { return r.Status == StatusFail }

// Validate runs G3 checks against PNG bytes and template output config.
func Validate(pngBytes []byte, cfg domain.OutputConfig) Result {
	img, err := decodePNGBounded(pngBytes)
	if err != nil {
		return Result{
			Status:  StatusFail,
			Codes:   []string{CodeDecodeFailed},
			Message: "png decode failed",
		}
	}

	var codes, warnings []string

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	// Fail closed: both canvas edges must be positive (C4 must not pass partial cfg).
	if cfg.CanvasWidth <= 0 || cfg.CanvasHeight <= 0 || w != cfg.CanvasWidth || h != cfg.CanvasHeight {
		codes = append(codes, CodeDimensionsMismatch)
	}

	hasAlpha := false
	unique := map[uint32]struct{}{}
	monoFail := false
	var sumY, sumY2 float64
	opaqueN := 0

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0xffff {
				hasAlpha = true
			}
			a8 := uint8(a16 >> 8)
			r8, g8, b8 := unpremultiply8(r16, g16, b16, a16)

			// Mono: any non-zero alpha pixel, on un-premultiplied channels (low-A
			// chroma must not collapse toward black via Color.RGBA() premul).
			if cfg.Monochrome && !monoFail && a16 > 0 {
				mx := max3(r8, g8, b8)
				mn := min3(r8, g8, b8)
				if int(mx)-int(mn) > MonochromeTolerance {
					monoFail = true
				}
			}
			// Blank: ignore near-transparent leftover RGB under cutouts.
			if a8 >= AlphaChromaFloor {
				q := uint32(r8>>4)<<8 | uint32(g8>>4)<<4 | uint32(b8>>4)
				unique[q] = struct{}{}
				y8 := 0.299*float64(r8) + 0.587*float64(g8) + 0.114*float64(b8)
				sumY += y8
				sumY2 += y8 * y8
				opaqueN++
			}
		}
	}

	if cfg.Monochrome && monoFail {
		codes = append(codes, CodeNotMonochrome)
	}
	if !hasAlpha {
		warnings = append(warnings, CodeNoTransparency)
	}
	if opaqueN == 0 {
		// Fully transparent (or below chroma floor) → blank.
		codes = append(codes, CodeBlankOrLowDetail)
	} else {
		mean := sumY / float64(opaqueN)
		variance := sumY2/float64(opaqueN) - mean*mean
		if len(unique) <= BlankMaxUniqueColors && variance < BlankVarianceFloor {
			codes = append(codes, CodeBlankOrLowDetail)
		}
	}

	if len(codes) > 0 {
		return Result{
			Status:   StatusFail,
			Codes:    codes,
			Warnings: warnings,
			Message:  "validation failed: " + strings.Join(codes, ", "),
		}
	}
	msg := "validation passed"
	if len(warnings) > 0 {
		msg = "validation passed with warnings: " + strings.Join(warnings, ", ")
	}
	return Result{
		Status:   StatusPass,
		Codes:    nil,
		Warnings: warnings,
		Message:  msg,
	}
}

// decodePNGBounded rejects impossible/oversize IHDR before full decode.
func decodePNGBounded(pngBytes []byte) (image.Image, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("invalid dimensions")
	}
	if cfg.Width > domain.MaxCanvasDimension || cfg.Height > domain.MaxCanvasDimension {
		return nil, fmt.Errorf("dimensions exceed max")
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// unpremultiply8 recovers approximately non-premultiplied 8-bit RGB from
// color.Color.RGBA() (which is alpha-premultiplied in 16-bit).
func unpremultiply8(r16, g16, b16, a16 uint32) (r8, g8, b8 uint8) {
	if a16 == 0 {
		return 0, 0, 0
	}
	if a16 == 0xffff {
		return uint8(r16 >> 8), uint8(g16 >> 8), uint8(b16 >> 8)
	}
	return uint8((r16 * 0xffff / a16) >> 8),
		uint8((g16 * 0xffff / a16) >> 8),
		uint8((b16 * 0xffff / a16) >> 8)
}

func max3(a, b, c uint8) uint8 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

func min3(a, b, c uint8) uint8 {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
