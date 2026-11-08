package ocr_test

import (
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/generation/ocr"
)

func TestNameHeuristic(t *testing.T) {
	t.Parallel()
	r := ocr.NameHeuristic("Burek")
	if r.Status != "manual_required" || r.Expected != "Burek" {
		t.Fatalf("%+v", r)
	}
	if ocr.NameHeuristic("").Status != "skipped" {
		t.Fatal("empty should skip")
	}
}
