package catalog

import (
	"fmt"
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
)

// parseNetMinor converts a human decimal into minor units with no rounding.
// ParseMinorUnits supplies the value. Empty separator sides, fraction-less
// thousands groups, and a group longer than 3 digits stay rejected (CR-24).
func parseNetMinor(raw string) (int64, error) {
	if amountShapeE1Rejects(raw) {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	minor, err := extapi.ParseMinorUnits(raw, 2)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	return minor, nil
}

func amountShapeE1Rejects(raw string) bool {
	lastComma := strings.LastIndexByte(raw, ',')
	lastDot := strings.LastIndexByte(raw, '.')
	if lastComma < 0 && lastDot < 0 {
		return false
	}
	if lastComma >= 0 && lastDot >= 0 {
		decAt := lastComma
		thou := byte('.')
		if lastDot > lastComma {
			decAt = lastDot
			thou = ','
		}
		if decAt == 0 || decAt == len(raw)-1 || strings.Count(raw, raw[decAt:decAt+1]) != 1 {
			return true
		}
		return thousandsGroupWiderThanE1(raw[:decAt], thou)
	}
	sep := byte('.')
	if lastComma >= 0 {
		sep = ','
	}
	if strings.Count(raw, string(sep)) != 1 {
		return true
	}
	at := strings.LastIndexByte(raw, sep)
	return at == 0 || at == len(raw)-1
}

func thousandsGroupWiderThanE1(whole string, sep byte) bool {
	if strings.IndexByte(whole, sep) < 0 {
		return false
	}
	parts := strings.Split(whole, string(sep))
	if len(parts[0]) < 1 || len(parts[0]) > 3 {
		return true
	}
	for _, part := range parts[1:] {
		if len(part) != 3 {
			return true
		}
	}
	return false
}
