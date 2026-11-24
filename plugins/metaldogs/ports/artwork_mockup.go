package ports

import "context"

// ArtworkMockupLookup finds the mockup that belongs to an approved artwork.
type ArtworkMockupLookup interface {
	// MockupAssetForArtwork returns the mockup of the generation job that stored artworkAssetID.
	// When no job has that asset, it returns the design's latest mockup whose artwork checksum matches.
	MockupAssetForArtwork(ctx context.Context, designID, artworkAssetID, artworkChecksum string) (mockupAssetID string, ok bool, err error)
}
