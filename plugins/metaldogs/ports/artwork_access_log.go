package ports

import (
	"context"
	"time"
)

// Artwork access kinds and channels stored in metaldogs_artwork_access_log.
const (
	ArtworkKindApproved   = "approved"
	ArtworkKindProduction = "production"
	ArtworkChannelAdmin   = "admin"
	ArtworkChannelCLI     = "cli"
)

// ArtworkAccessEvent is one issued URL. It carries no URL and no storage key.
type ArtworkAccessEvent struct {
	OrderID     string
	VariantID   string
	Kind        string
	Actor       string
	Channel     string
	RequestedAt time.Time
}

// ArtworkAccessLog appends one row per issued URL. A failed write writes nothing.
type ArtworkAccessLog interface {
	Append(ctx context.Context, events []ArtworkAccessEvent) error
}
