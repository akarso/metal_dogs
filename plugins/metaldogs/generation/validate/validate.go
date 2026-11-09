// Package validate implements G3 deterministic checks on provider PNG output.
package validate

import (
	"bytes"
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

// MonochromeTolerance is the max (maxRGB−minRGB) on 8-bit channels for monochrome pass.
const MonochromeTolerance = 16

// BlankMaxUniqueColors: after 4-bit-per-channel quantize, ≤ this many colors is
// a blank candidate (also requires low luminance variance — see BlankVarianceFloor).
const BlankMaxUniqueColors = 4

// BlankVarianceFloor is the minimum population luminance variance (8-bit Y) required
// to pass when unique quantized colors ≤ BlankMaxUniqueColors.
const BlankVarianceFloor = 80.0

// Result is the outcome of Validate.
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
	var codes, warnings []string

	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return Result{
			Status:  StatusFail,
			Codes:   []string{CodeDecodeFailed},
			Message: "png decode failed: " + err.Error(),
		}
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if cfg.CanvasWidth > 0 && cfg.CanvasHeight > 0 {
		if w != cfg.CanvasWidth || h != cfg.CanvasHeight {
			codes = append(codes, CodeDimensionsMismatch)
		}
	}

	hasAlpha := false
	unique := map[uint32]struct{}{}
	monoFail := false
	var sumY, sumY2 float64
	n := 0

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0xffff {
				hasAlpha = true
			}
			r8, g8, b8 := uint8(r16>>8), uint8(g16>>8), uint8(b16>>8)
			if cfg.Monochrome && !monoFail {
				mx := max3(r8, g8, b8)
				mn := min3(r8, g8, b8)
				if int(mx)-int(mn) > MonochromeTolerance {
					monoFail = true
				}
			}
			// Quantize to 4 bits/channel for blank detection.
			q := uint32(r8>>4)<<8 | uint32(g8>>4)<<4 | uint32(b8>>4)
			unique[q] = struct{}{}
			// Rec. 601 luminance approximation on 8-bit channels.
			y8 := 0.299*float64(r8) + 0.587*float64(g8) + 0.114*float64(b8)
			sumY += y8
			sumY2 += y8 * y8
			n++
		}
	}

	if cfg.Monochrome && monoFail {
		codes = append(codes, CodeNotMonochrome)
	}
	if !hasAlpha {
		warnings = append(warnings, CodeNoTransparency)
		codes = append(codes, CodeNoTransparency)
	}
	if n > 0 {
		mean := sumY / float64(n)
		variance := sumY2/float64(n) - mean*mean
		if len(unique) <= BlankMaxUniqueColors && variance < BlankVarianceFloor {
			codes = append(codes, CodeBlankOrLowDetail)
		}
	}

	hard := hardFailCodes(codes)
	if len(hard) > 0 {
		return Result{
			Status:   StatusFail,
			Codes:    codes,
			Warnings: warnings,
			Message:  "validation failed: " + strings.Join(hard, ", "),
		}
	}
	msg := "validation passed"
	if len(warnings) > 0 {
		msg = "validation passed with warnings: " + strings.Join(warnings, ", ")
	}
	return Result{
		Status:   StatusPass,
		Codes:    codes,
		Warnings: warnings,
		Message:  msg,
	}
}

func hardFailCodes(codes []string) []string {
	var out []string
	for _, c := range codes {
		if c == CodeNoTransparency {
			continue // warning until ADR-0008 Accept
		}
		out = append(out, c)
	}
	return out
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
