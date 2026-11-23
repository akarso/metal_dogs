package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// OrderArtworkTTL is the fixed signed-URL lifetime for artwork access.
const OrderArtworkTTL = 300 * time.Second

// ArtworkActorCLI is the actor recorded for shell access.
const ArtworkActorCLI = "cli"

// ErrOrderNotFound means Host.Orders().FindByID returned no order.
var ErrOrderNotFound = errors.New("not found")

// ErrArtworkAccessFailed means no URL is returned. The cause is not included:
// provider and storage errors can carry a storage key.
var ErrArtworkAccessFailed = errors.New("order artwork access failed")

// ArtworkLogger records one line per access call. Fields are order id and actor.
type ArtworkLogger interface {
	Info(event string, fields map[string]interface{})
	Error(event string, err error, fields map[string]interface{})
}

// OrderArtwork is the operator view of one order's snapshot artwork.
type OrderArtwork struct {
	OrderID string             `json:"order_id"`
	Lines   []OrderArtworkLine `json:"lines"`
}

// OrderArtworkLine is one snapshot line. Production is nil when the snapshot has none.
type OrderArtworkLine struct {
	VariantID       string            `json:"variant_id"`
	SKU             string            `json:"sku"`
	TemplateSlug    string            `json:"template_slug"`
	TemplateVersion int               `json:"template_version"`
	Texts           map[string]string `json:"texts"`
	Provider        string            `json:"provider"`
	Model           string            `json:"model"`
	PromptVersion   string            `json:"prompt_version"`
	Approved        OrderArtworkFile  `json:"approved"`
	Production      *OrderArtworkFile `json:"production"`
}

// OrderArtworkFile is a short-lived signed URL and the snapshot checksum.
type OrderArtworkFile struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Checksum  string    `json:"checksum"`
}

// OrderArtworkAccess signs snapshot artwork and records each issued URL.
type OrderArtworkAccess struct {
	Orders    ports.OrderLookup
	Snapshots ports.OrderDesignSnapshotRepository
	Assets    ports.AssetRepository
	Store     ports.AssetStore
	Log       ports.ArtworkAccessLog
	Clock     ports.Clock
	Logger    ArtworkLogger
}

// Access returns metadata and signed URLs. A failed audit write returns no URLs.
func (s *OrderArtworkAccess) Access(ctx context.Context, orderID, actor, channel string) (OrderArtwork, error) {
	if s == nil || s.Orders == nil || s.Snapshots == nil || s.Assets == nil || s.Store == nil || s.Log == nil || s.Clock == nil {
		return OrderArtwork{}, ErrArtworkAccessFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	orderID = strings.TrimSpace(orderID)
	actor = strings.TrimSpace(actor)
	channel = strings.TrimSpace(channel)
	if actor == "" || (channel != ports.ArtworkChannelAdmin && channel != ports.ArtworkChannelCLI) {
		return OrderArtwork{}, ErrArtworkAccessFailed
	}
	if err := canonicalOrderID(orderID); err != nil {
		s.logCall(orderID, actor, true)
		return OrderArtwork{}, err
	}
	exists, err := s.Orders.Exists(ctx, orderID)
	if err != nil {
		s.logCall(orderID, actor, true)
		return OrderArtwork{}, ErrArtworkAccessFailed
	}
	if !exists {
		s.logCall(orderID, actor, false)
		return OrderArtwork{}, ErrOrderNotFound
	}
	snaps, err := s.Snapshots.ListByOrder(ctx, orderID)
	if err != nil {
		s.logCall(orderID, actor, true)
		return OrderArtwork{}, ErrArtworkAccessFailed
	}
	now := s.Clock.Now().UTC()
	expires := now.Add(OrderArtworkTTL)
	lines := make([]OrderArtworkLine, 0, len(snaps))
	events := make([]ports.ArtworkAccessEvent, 0, len(snaps)*2)
	for _, snap := range snaps {
		line, issued, err := s.signLine(ctx, snap, actor, channel, now, expires)
		if err != nil {
			s.logCall(orderID, actor, true)
			return OrderArtwork{}, ErrArtworkAccessFailed
		}
		lines = append(lines, line)
		events = append(events, issued...)
	}
	if err := s.Log.Append(ctx, events); err != nil {
		s.logCall(orderID, actor, true)
		return OrderArtwork{}, ErrArtworkAccessFailed
	}
	s.logCall(orderID, actor, false)
	return OrderArtwork{OrderID: orderID, Lines: lines}, nil
}

func (s *OrderArtworkAccess) signLine(ctx context.Context, snap domain.OrderDesignSnapshot, actor, channel string, now, expires time.Time) (OrderArtworkLine, []ports.ArtworkAccessEvent, error) {
	approvedURL, err := s.signAsset(ctx, snap.ArtworkAssetID)
	if err != nil {
		return OrderArtworkLine{}, nil, err
	}
	line := OrderArtworkLine{
		VariantID:       snap.VariantID,
		SKU:             snap.SKU,
		TemplateSlug:    snap.TemplateSlug,
		TemplateVersion: snap.TemplateVersion,
		Texts:           copyArtworkTexts(snap.Texts),
		Provider:        snap.Provider,
		Model:           snap.Model,
		PromptVersion:   snap.PromptVersion,
		Approved: OrderArtworkFile{
			URL:       approvedURL,
			ExpiresAt: expires,
			Checksum:  snap.ArtworkChecksum,
		},
	}
	events := []ports.ArtworkAccessEvent{artworkEvent(snap, ports.ArtworkKindApproved, actor, channel, now)}
	if snap.ProductionAssetID == "" {
		return line, events, nil
	}
	prodURL, err := s.signAsset(ctx, snap.ProductionAssetID)
	if err != nil {
		return OrderArtworkLine{}, nil, err
	}
	line.Production = &OrderArtworkFile{
		URL:       prodURL,
		ExpiresAt: expires,
		Checksum:  snap.ProductionChecksum,
	}
	events = append(events, artworkEvent(snap, ports.ArtworkKindProduction, actor, channel, now))
	return line, events, nil
}

func (s *OrderArtworkAccess) signAsset(ctx context.Context, assetID string) (string, error) {
	asset, ok, err := s.Assets.GetByID(ctx, assetID)
	if err != nil || !ok || strings.TrimSpace(asset.StorageKey) == "" {
		return "", ErrArtworkAccessFailed
	}
	url, err := s.Store.SignedGetURL(ctx, asset.StorageKey, OrderArtworkTTL)
	if err != nil || strings.TrimSpace(url) == "" {
		return "", ErrArtworkAccessFailed
	}
	return url, nil
}

func artworkEvent(snap domain.OrderDesignSnapshot, kind, actor, channel string, at time.Time) ports.ArtworkAccessEvent {
	return ports.ArtworkAccessEvent{
		OrderID:     snap.OrderID,
		VariantID:   snap.VariantID,
		Kind:        kind,
		Actor:       actor,
		Channel:     channel,
		RequestedAt: at,
	}
}

func copyArtworkTexts(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *OrderArtworkAccess) logCall(orderID, actor string, failed bool) {
	if s == nil || s.Logger == nil {
		return
	}
	fields := map[string]interface{}{
		"order_id": orderID,
		"actor":    actor,
	}
	if failed {
		s.Logger.Error("metaldogs.order_artwork.access", ErrArtworkAccessFailed, fields)
		return
	}
	s.Logger.Info("metaldogs.order_artwork.access", fields)
}
