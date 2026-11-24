package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

func TestOrderThumbnailBytes_FaultsStayOutOfTheMiss(t *testing.T) {
	t.Parallel()
	const (
		key      = "0123456789abcdef0123456789abcdef"
		orderID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		variant  = "11111111-1111-4111-8111-111111111111"
		storeKey = "private/blobs/metaldogs/thumbnail/2026/11/secret-object.png"
	)
	token := thumbnail.Token(key, orderID, variant)
	snapErr := errors.New("snapshots down " + storeKey)
	assetErr := errors.New("assets down " + storeKey)
	readErr := errors.New("read down " + storeKey)

	cases := []struct {
		name  string
		svc   OrderThumbnail
		cause string
		miss  bool
	}{
		{
			name: "snapshot",
			svc: OrderThumbnail{
				Snapshots: thumbSnap{err: snapErr},
				Assets:    thumbAssets{},
				Store:     thumbStore{},
				Key:       key,
			},
			cause: OrderThumbnailCauseSnapshot,
		},
		{
			name: "asset",
			svc: OrderThumbnail{
				Snapshots: thumbSnap{found: true, snap: domain.OrderDesignSnapshot{ThumbnailAssetID: "tid"}},
				Assets:    thumbAssets{err: assetErr},
				Store:     thumbStore{},
				Key:       key,
			},
			cause: OrderThumbnailCauseAsset,
		},
		{
			name: "object",
			svc: OrderThumbnail{
				Snapshots: thumbSnap{found: true, snap: domain.OrderDesignSnapshot{ThumbnailAssetID: "tid"}},
				Assets: thumbAssets{found: true, asset: domain.Asset{
					ID: "tid", StorageKey: storeKey, MimeType: "image/png",
				}},
				Store: thumbStore{err: readErr},
				Key:   key,
			},
			cause: OrderThumbnailCauseObject,
		},
		{
			name: "missing asset row",
			svc: OrderThumbnail{
				Snapshots: thumbSnap{found: true, snap: domain.OrderDesignSnapshot{ThumbnailAssetID: "tid"}},
				Assets:    thumbAssets{},
				Store:     thumbStore{},
				Key:       key,
			},
			miss: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := tc.svc.Bytes(t.Context(), token)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), storeKey) || strings.Contains(err.Error(), token) {
				t.Fatalf("error text leaked: %v", err)
			}
			var fault *OrderThumbnailFault
			if tc.miss {
				if !errors.Is(err, ErrOrderThumbnailMiss) || errors.As(err, &fault) {
					t.Fatalf("%v", err)
				}
				return
			}
			if !errors.As(err, &fault) || fault.OrderID != orderID || fault.VariantID != variant || fault.Cause != tc.cause {
				t.Fatalf("%+v", fault)
			}
			if errors.Is(err, ErrOrderThumbnailMiss) {
				t.Fatal("fault collapsed into miss")
			}
		})
	}
}

type thumbSnap struct {
	snap  domain.OrderDesignSnapshot
	found bool
	err   error
}

func (s thumbSnap) Insert(context.Context, domain.OrderDesignSnapshot) (bool, error) {
	return false, s.err
}
func (s thumbSnap) CommitOrder(context.Context, []domain.OrderDesignSnapshot, []domain.Asset, time.Time) ([]bool, []bool, error) {
	return nil, nil, s.err
}
func (s thumbSnap) Get(context.Context, string, string) (domain.OrderDesignSnapshot, bool, error) {
	return s.snap, s.found, s.err
}
func (s thumbSnap) ListByOrder(context.Context, string) ([]domain.OrderDesignSnapshot, error) {
	return nil, s.err
}
func (s thumbSnap) ListUnfilledProduction(context.Context, string) ([]domain.OrderDesignSnapshot, error) {
	return nil, s.err
}
func (s thumbSnap) FillProduction(context.Context, string, string, string, string) error {
	return s.err
}

type thumbAssets struct {
	asset domain.Asset
	found bool
	err   error
}

func (a thumbAssets) Insert(context.Context, domain.Asset) error { return a.err }
func (a thumbAssets) GetByID(context.Context, string) (domain.Asset, bool, error) {
	return a.asset, a.found, a.err
}
func (a thumbAssets) SoftDelete(context.Context, string, time.Time) error { return a.err }
func (a thumbAssets) BindOwnerRef(context.Context, string, string, time.Time) error {
	return a.err
}
func (a thumbAssets) ClearOwnerRef(context.Context, string, string) error { return a.err }
func (a thumbAssets) ClearStorageKey(context.Context, string) error       { return a.err }

type thumbStore struct {
	err error
}

func (s thumbStore) Put(context.Context, string, io.Reader, string) error { return s.err }
func (s thumbStore) Read(context.Context, string) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []byte("png"), nil
}
func (s thumbStore) Head(context.Context, string) error { return s.err }
func (s thumbStore) SignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", s.err
}
func (s thumbStore) Delete(context.Context, string) error { return s.err }

var (
	_ ports.OrderDesignSnapshotRepository = thumbSnap{}
	_ ports.AssetRepository               = thumbAssets{}
	_ ports.AssetStore                    = thumbStore{}
)
