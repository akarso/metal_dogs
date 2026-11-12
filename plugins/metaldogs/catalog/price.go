package catalog

import (
	"fmt"
	"math"
	"strconv"
)

// parseNetMinor converts a simple decimal (one comma or one dot, at most two
// fractional digits) into minor units.
func parseNetMinor(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}
	sep := -1
	for i, r := range raw {
		switch r {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		case ',', '.':
			if sep >= 0 {
				return 0, fmt.Errorf("invalid amount %q", raw)
			}
			sep = i
		default:
			return 0, fmt.Errorf("invalid amount %q", raw)
		}
	}
	whole := raw
	frac := "00"
	if sep >= 0 {
		whole = raw[:sep]
		frac = raw[sep+1:]
		if whole == "" || frac == "" || len(frac) > 2 {
			return 0, fmt.Errorf("invalid amount %q", raw)
		}
		if len(frac) == 1 {
			frac += "0"
		}
	}
	if whole == "" {
		return 0, fmt.Errorf("invalid amount %q", raw)
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
	return w*100 + f, nil
}
