package ports

import "github.com/google/uuid"

// IDGenerator produces opaque string IDs.
type IDGenerator interface {
	New() string
}

// UUIDGenerator uses crypto UUID v4 (production default).
type UUIDGenerator struct{}

// New returns a canonical UUID v4 (lowercase hex, RFC 4122), the same shape as core id.New.
func (UUIDGenerator) New() string { return uuid.NewString() }
