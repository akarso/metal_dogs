package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestArtworkAccessLogRepo_BadEventWritesNothing(t *testing.T) {
	db := testutil.OpenIsolated(t, "artwork_access_log")
	repo := postgres.NewArtworkAccessLogRepo(db)
	ctx := context.Background()
	at := time.Date(2026, 11, 23, 16, 0, 0, 0, time.UTC)
	orderID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	good := ports.ArtworkAccessEvent{
		OrderID: orderID, VariantID: "11111111-1111-4111-8111-111111111111",
		Kind: ports.ArtworkKindApproved, Actor: "cli", Channel: ports.ArtworkChannelCLI,
		RequestedAt: at,
	}
	if err := repo.Append(ctx, nil); err != nil {
		t.Fatal(err)
	}
	bad := []ports.ArtworkAccessEvent{
		{OrderID: "not-a-uuid", VariantID: good.VariantID, Kind: good.Kind, Actor: good.Actor, Channel: good.Channel, RequestedAt: at},
		{OrderID: orderID, VariantID: "  ", Kind: good.Kind, Actor: good.Actor, Channel: good.Channel, RequestedAt: at},
		{OrderID: orderID, VariantID: good.VariantID, Kind: good.Kind, Actor: " ", Channel: good.Channel, RequestedAt: at},
		{OrderID: orderID, VariantID: good.VariantID, Kind: good.Kind, Actor: good.Actor, Channel: good.Channel},
		{OrderID: orderID, VariantID: good.VariantID, Kind: "preview", Actor: good.Actor, Channel: good.Channel, RequestedAt: at},
		{OrderID: orderID, VariantID: good.VariantID, Kind: good.Kind, Actor: good.Actor, Channel: "shell", RequestedAt: at},
	}
	for _, event := range bad {
		if err := repo.Append(ctx, []ports.ArtworkAccessEvent{good, event}); err == nil {
			t.Fatalf("accepted %+v", event)
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metaldogs_artwork_access_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows %d", n)
	}
	if err := repo.Append(ctx, []ports.ArtworkAccessEvent{good}); err != nil {
		t.Fatal(err)
	}
	var kind, actor, channel string
	if err := db.QueryRowContext(ctx, `
		SELECT kind, actor, channel FROM metaldogs_artwork_access_log WHERE order_id = $1
	`, orderID).Scan(&kind, &actor, &channel); err != nil {
		t.Fatal(err)
	}
	if kind != ports.ArtworkKindApproved || actor != "cli" || channel != ports.ArtworkChannelCLI {
		t.Fatalf("row %s %s %s", kind, actor, channel)
	}
}
