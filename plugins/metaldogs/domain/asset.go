package domain

import "time"

// Asset type values (snake_case); align with metaldogs_assets CHECK.
const (
	AssetTypeSource     = "source"
	AssetTypeArtwork    = "artwork"
	AssetTypeMockup     = "mockup"
	AssetTypeProduction = "production"
	AssetTypeReference  = "reference"
	AssetTypeThumbnail  = "thumbnail"
)

// Asset is private media metadata (blobs live in AssetStore).
type Asset struct {
	ID          string
	Type        string
	StorageKey  string
	MimeType    string
	SizeBytes   int64
	Width       int
	Height      int
	Checksum    string
	CreatedAt   time.Time
	DeleteAfter time.Time
	DeletedAt   *time.Time
}
