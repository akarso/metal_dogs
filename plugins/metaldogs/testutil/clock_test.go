package testutil_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestFakeClock_Advance(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := testutil.NewFakeClock(start)
	if !c.Now().Equal(start) {
		t.Fatalf("Now = %v", c.Now())
	}
	c.Advance(2 * time.Hour)
	if want := start.Add(2 * time.Hour); !c.Now().Equal(want) {
		t.Fatalf("After Advance Now = %v, want %v", c.Now(), want)
	}
}

func TestSeqGenerator_Deterministic(t *testing.T) {
	t.Parallel()
	g := &testutil.SeqGenerator{Prefix: "x"}
	if a, b := g.New(), g.New(); a != "x-0001" || b != "x-0002" {
		t.Fatalf("got %q %q", a, b)
	}
}
