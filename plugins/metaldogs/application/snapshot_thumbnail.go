package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

// thumbnailAssetNamespace is the UUID v5 namespace for an order line's thumbnail asset id.
var thumbnailAssetNamespace = uuid.MustParse("0e6a0000-0000-4000-8000-000000000e6a")

// ThumbnailAssetID is the UUID v5 of orderID and variantID.
// A second snapshot of the same line addresses the same asset.
func ThumbnailAssetID(orderID, variantID string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(orderID))
	if err != nil {
		return "", fmt.Errorf("metaldogs snapshot: order id: %w", err)
	}
	variantID = strings.TrimSpace(variantID)
	if variantID == "" {
		return "", fmt.Errorf("metaldogs snapshot: variant id required")
	}
	return uuid.NewSHA1(thumbnailAssetNamespace, []byte(parsed.String()+"|"+variantID)).String(), nil
}

func (s *SnapshotOrder) attachThumbnail(ctx context.Context, snap domain.OrderDesignSnapshot, design domain.Design) (domain.OrderDesignSnapshot, domain.Asset, bool, error) {
	_, exists, err := s.Snapshots.Get(ctx, snap.OrderID, snap.VariantID)
	if err != nil {
		return domain.OrderDesignSnapshot{}, domain.Asset{}, false, &SnapshotOrderError{
			OrderID:   snap.OrderID,
			VariantID: snap.VariantID,
			DesignID:  snap.DesignID,
			Err:       err,
		}
	}
	if exists {
		return snap, domain.Asset{}, false, nil
	}
	if s.Mockups == nil || s.Assets == nil || s.Store == nil {
		return snap, domain.Asset{}, true, nil
	}
	mockupID, ok, err := s.Mockups.MockupAssetForArtwork(ctx, design.ID, snap.ArtworkAssetID, snap.ArtworkChecksum)
	if err != nil || !ok {
		return snap, domain.Asset{}, true, nil
	}
	asset, err := s.makeThumbnail(ctx, snap, design, mockupID)
	if err != nil {
		return snap, domain.Asset{}, true, nil
	}
	next, err := snap.WithThumbnail(mockupID, asset.ID)
	if err != nil {
		return snap, domain.Asset{}, true, nil
	}
	return next, asset, false, nil
}

func (s *SnapshotOrder) makeThumbnail(ctx context.Context, snap domain.OrderDesignSnapshot, design domain.Design, mockupID string) (domain.Asset, error) {
	mock, ok, err := s.Assets.GetByID(ctx, mockupID)
	if err != nil {
		return domain.Asset{}, err
	}
	if !ok || mock.Type != domain.AssetTypeMockup || mock.StorageKey == "" {
		return domain.Asset{}, fmt.Errorf("metaldogs snapshot: mockup unavailable")
	}
	raw, err := s.Store.Read(ctx, mock.StorageKey)
	if err != nil {
		return domain.Asset{}, err
	}
	pngBytes, w, h, err := thumbnail.PNG(raw, thumbnail.MaxEdge)
	if err != nil {
		return domain.Asset{}, err
	}
	assetID, err := ThumbnailAssetID(snap.OrderID, snap.VariantID)
	if err != nil {
		return domain.Asset{}, err
	}
	now := snap.CreatedAt.UTC()
	key, err := ports.ObjectKey(domain.AssetTypeThumbnail, assetID, "png", now)
	if err != nil {
		return domain.Asset{}, err
	}
	if err := s.Store.Put(ctx, key, bytes.NewReader(pngBytes), "image/png"); err != nil {
		return domain.Asset{}, err
	}
	sum := sha256.Sum256(pngBytes)
	// owner_ref stays empty: 009 allows one live row per (owner_ref, type), and two lines can share a design.
	return domain.Asset{
		ID:          assetID,
		Type:        domain.AssetTypeThumbnail,
		StorageKey:  key,
		MimeType:    "image/png",
		SizeBytes:   int64(len(pngBytes)),
		Width:       w,
		Height:      h,
		Checksum:    ChecksumSHA256Prefix + hex.EncodeToString(sum[:]),
		CreatedAt:   now,
		DeleteAfter: thumbnailDeleteAfter(design, now),
	}, nil
}

func thumbnailDeleteAfter(design domain.Design, now time.Time) time.Time {
	if design.ExpiresAt.After(now) {
		return design.ExpiresAt.UTC()
	}
	return now.Add(domain.DesignRetentionUnordered)
}
