// Package ocr records a simple dog-name check slot for G1 (automation later).
package ocr

import "strings"

// Result is a non-authoritative name check for scorecards.
type Result struct {
	Status   string `json:"status"` // manual_required | match | mismatch | skipped
	Expected string `json:"expected,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// NameHeuristic is a placeholder until a real OCR path exists (G3+).
// It never inspects image bytes; humans fill scorecard name_spelling_ok.
func NameHeuristic(dogName string) Result {
	name := strings.TrimSpace(dogName)
	if name == "" {
		return Result{Status: "skipped", Notes: "no dog_name on template"}
	}
	return Result{
		Status:   "manual_required",
		Expected: name,
		Notes:    "G1: confirm spelling/legibility by eye; automate in G3",
	}
}
