package testutil_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestFakeClock_AdvanceAndSet(t *testing.T) {
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
	next := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	c.Set(next)
	if !c.Now().Equal(next) {
		t.Fatalf("After Set Now = %v, want %v", c.Now(), next)
	}
}

func TestSeqGenerator_Deterministic(t *testing.T) {
	t.Parallel()
	g := testutil.NewSeqGenerator("x")
	if a, b := g.New(), g.New(); a != "x-0001" || b != "x-0002" {
		t.Fatalf("got %q %q", a, b)
	}
}

func TestFixedGenerator(t *testing.T) {
	t.Parallel()
	g := &testutil.FixedGenerator{ID: "fixed-a"}
	if a, b := g.New(), g.New(); a != "fixed-a" || b != "fixed-a" {
		t.Fatalf("got %q %q", a, b)
	}
	empty := &testutil.FixedGenerator{}
	if empty.New() != "fixed-id" {
		t.Fatalf("default FixedGenerator = %q", empty.New())
	}
}
