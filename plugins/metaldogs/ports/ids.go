package ports

import "github.com/akarso/shopanda/internal/platform/id"

// IDGenerator produces opaque string IDs.
type IDGenerator interface {
	New() string
}

// UUIDGenerator uses crypto UUID v4 (production default).
type UUIDGenerator struct{}

// New returns id.New().
func (UUIDGenerator) New() string { return id.New() }
