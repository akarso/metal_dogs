package testutil

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// SeqGenerator returns deterministic IDs: prefix-0001, prefix-0002, …
// Always use a pointer (NewSeqGenerator); copying by value resets the counter
// independently because atomic.Uint64 must not be copied after first use.
type SeqGenerator struct {
	Prefix string
	n      atomic.Uint64
}

// NewSeqGenerator returns a pointer SeqGenerator (required; do not copy by value).
func NewSeqGenerator(prefix string) *SeqGenerator {
	return &SeqGenerator{Prefix: prefix}
}

var _ ports.IDGenerator = (*SeqGenerator)(nil)

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

var _ ports.IDGenerator = (*FixedGenerator)(nil)

// New returns the fixed ID.
func (g *FixedGenerator) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ID == "" {
		g.ID = "fixed-id"
	}
	return g.ID
}
