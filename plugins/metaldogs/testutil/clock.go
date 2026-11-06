package testutil

import (
	"sync"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// FakeClock is a mutable clock for tests.
type FakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// NewFakeClock returns a FakeClock starting at t (normalized to UTC).
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{t: t.UTC()}
}

var _ ports.Clock = (*FakeClock)(nil)

// Now returns the frozen time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Set replaces the clock time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t.UTC()
}
