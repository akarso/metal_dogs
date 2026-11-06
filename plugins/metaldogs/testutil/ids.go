package testutil

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/akarso/shopanda/internal/platform/id"
)

// IDGenerator produces opaque string IDs.
type IDGenerator interface {
	New() string
}

// UUIDGenerator uses crypto UUID v4 (production default).
type UUIDGenerator struct{}

// New returns id.New().
func (UUIDGenerator) New() string { return id.New() }

// SeqGenerator returns deterministic IDs: prefix-0001, prefix-0002, …
type SeqGenerator struct {
	Prefix string
	n      atomic.Uint64
}

// New returns the next sequential ID.
func (g *SeqGenerator) New() string {
	v := g.n.Add(1)
	prefix := g.Prefix
	if prefix == "" {
		prefix = "id"
	}
	return fmt.Sprintf("%s-%04d", prefix, v)
}

// FixedGenerator always returns the same ID (for single-entity tests).
type FixedGenerator struct {
	mu sync.Mutex
	ID string
}

// New returns the fixed ID.
func (g *FixedGenerator) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ID == "" {
		g.ID = "fixed-id"
	}
	return g.ID
}
