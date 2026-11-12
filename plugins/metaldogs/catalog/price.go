package catalog

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// parseNetMinor converts a human decimal into minor units with no rounding.
// One separator is decimal; if both comma and dot appear, the last is decimal and the other groups thousands.
func parseNetMinor(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	for _, r := range raw {
		if (r < '0' || r > '9') && r != ',' && r != '.' {
			return 0, fmt.Errorf("invalid amount %q", raw)
		}
	}
	lastComma := strings.LastIndexByte(raw, ',')
	lastDot := strings.LastIndexByte(raw, '.')
	if lastComma >= 0 && lastDot >= 0 {
		decAt := lastComma
		decSep, thouSep := byte(','), byte('.')
		if lastDot > lastComma {
			decAt = lastDot
			decSep, thouSep = '.', ','
		}
		if strings.Count(raw, string(decSep)) != 1 {
			return 0, fmt.Errorf("invalid amount %q", raw)
		}
		whole, ok := stripThousands(raw[:decAt], thouSep)
		if !ok || strings.IndexByte(raw[decAt+1:], decSep) >= 0 || strings.IndexByte(raw[decAt+1:], thouSep) >= 0 {
			return 0, fmt.Errorf("invalid amount %q", raw)
		}
		return minorFromParts(raw, whole, raw[decAt+1:])
	}
	sep := byte(0)
	if lastComma >= 0 {
		sep = ','
	}
	if lastDot >= 0 {
		sep = '.'
	}
	if sep == 0 {
		return minorFromParts(raw, raw, "00")
	}
	if strings.Count(raw, string(sep)) != 1 {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	whole, frac, _ := strings.Cut(raw, string(sep))
	return minorFromParts(raw, whole, frac)
}

func stripThousands(whole string, sep byte) (string, bool) {
	if whole == "" || strings.IndexByte(whole, sep) < 0 {
		if whole == "" || !digitsOnly(whole) {
			return "", false
		}
		return whole, true
	}
	parts := strings.Split(whole, string(sep))
	if len(parts[0]) < 1 || len(parts[0]) > 3 || !digitsOnly(parts[0]) {
		return "", false
	}
	for _, part := range parts[1:] {
		if len(part) != 3 || !digitsOnly(part) {
			return "", false
		}
	}
	return strings.Join(parts, ""), true
}

func minorFromParts(raw, whole, frac string) (int64, error) {
	if whole == "" || !digitsOnly(whole) || frac == "" || len(frac) > 2 || !digitsOnly(frac) {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	if len(frac) == 1 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	if w > (math.MaxInt64-f)/100 {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	minor := w*100 + f
	if minor == 0 {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	return minor, nil
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
