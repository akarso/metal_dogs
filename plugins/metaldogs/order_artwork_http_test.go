package metaldogs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	adminhttp "github.com/akarso/shopanda/internal/interfaces/http/admin"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/hostorders"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOrderArtworkAdmin_RegisteredOnlyWhenFlagOn(t *testing.T) {
	t.Parallel()
	off := testApp(t, true)
	initMetalDogs(t, off)
	if route, ok := artworkAdminRoute(off); ok {
		t.Fatalf("flag off registered %+v", route.Pattern)
	}
	if off.PermissionRegistry().Has(identity.RoleAdmin, metaldogs.PermissionSnapshotsRead) {
		t.Fatal("permission registered while order_snapshot.enabled is false")
	}

	on := testApp(t, true)
	on.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	initMetalDogs(t, on)
	route, ok := artworkAdminRoute(on)
	if !ok || route.Permission != metaldogs.PermissionSnapshotsRead || route.Handler == nil {
		t.Fatalf("flag on route %+v", route)
	}
	reg := on.PermissionRegistry()
	perm := rbac.Permission(metaldogs.PermissionSnapshotsRead)
	if !reg.Has(identity.RoleAdmin, perm) || reg.Has(identity.RoleManager, perm) || reg.Has(identity.RoleEditor, perm) || reg.Has(identity.RoleSupport, perm) {
		t.Fatal("permission must be admin only")
	}
}

func TestOrderArtworkAdminRoute(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_artwork_admin")
	start := time.Date(2026, 11, 23, 16, 0, 0, 0, time.UTC)
	clock := testutil.NewFakeClock(start)
	const (
		adminID      = "admin-42"
		orderFull    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		orderPreview = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		orderEmpty   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		orderMissing = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
		variantFull  = "11111111-1111-4111-8111-111111111111"
		variantPrev  = "22222222-2222-4222-8222-222222222222"
		designFull   = "33333333-3333-4333-8333-333333333333"
		versionFull  = "44444444-4444-4444-8444-444444444444"
		designPrev   = "55555555-5555-4555-8555-555555555555"
		versionPrev  = "66666666-6666-4666-8666-666666666666"
		artFull      = "77777777-7777-4777-8777-777777777777"
		prodFull     = "88888888-8888-4888-8888-888888888888"
		artPrev      = "99999999-9999-4999-8999-999999999999"
	)
	artKey := "private/blobs/metaldogs/artwork/2026/11/" + artFull + ".png"
	prodKey := "private/blobs/metaldogs/production/2026/11/" + prodFull + ".pdf"
	prevKey := "private/blobs/metaldogs/artwork/2026/11/" + artPrev + ".png"
	store := &expiringStore{clock: clock, body: map[string][]byte{
		artKey:  []byte("approved-bytes"),
		prodKey: []byte("production-bytes"),
		prevKey: []byte("preview-only"),
	}}
	insertArtworkAsset(t, db, artFull, domain.AssetTypeArtwork, artKey, start)
	insertArtworkAsset(t, db, prodFull, domain.AssetTypeProduction, prodKey, start)
	insertArtworkAsset(t, db, artPrev, domain.AssetTypeArtwork, prevKey, start)
	insertArtworkSnapshot(t, db, orderFull, variantFull, designFull, versionFull, artFull, "sha256:approved", prodFull, "sha256:production", start)
	insertArtworkSnapshot(t, db, orderPreview, variantPrev, designPrev, versionPrev, artPrev, "sha256:preview", "", "", start)

	orders := &artworkOrders{ids: map[string]struct{}{
		orderFull: {}, orderPreview: {}, orderEmpty: {},
	}}
	lookup, ok := hostorders.New(orders).(ports.OrderLookup)
	if !ok {
		t.Fatal("host orders source is not an OrderLookup")
	}
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	p.SetOrderLookup(lookup)
	p.SetAssetStore(store)
	p.SetSnapshotClock(clock)
	h := mountArtworkAdmin(t, app)

	status, body := artworkDo(h, "", orderFull)
	if status != http.StatusUnauthorized || strings.Contains(body, "http") || strings.Contains(body, "private/blobs") {
		t.Fatalf("anon status=%d body=%s", status, body)
	}

	status, body = artworkDo(h, "test-token:manager-1:manager", orderFull)
	if status != http.StatusForbidden || strings.Contains(body, "http") || strings.Contains(body, artKey) {
		t.Fatalf("manager status=%d body=%s", status, body)
	}
	if n := countAudit(t, db, orderFull); n != 0 {
		t.Fatalf("refused call wrote %d audit rows", n)
	}

	status, body = artworkDo(h, "test-token:"+adminID+":admin", orderFull)
	if status != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", status, body)
	}
	var full application.OrderArtwork
	if err := json.Unmarshal([]byte(body), &full); err != nil {
		t.Fatal(err)
	}
	if full.OrderID != orderFull || len(full.Lines) != 1 {
		t.Fatalf("view %+v", full)
	}
	line := full.Lines[0]
	if line.VariantID != variantFull || line.SKU != "TEE-BLK-M" || line.TemplateSlug != "black-tee" || line.TemplateVersion != 3 ||
		line.Texts["dog_name"] != "Burek" || line.Provider != "openai" || line.Model != "gpt-image" || line.PromptVersion != "prompt-v7" ||
		line.Approved.Checksum != "sha256:approved" || line.Production == nil || line.Production.Checksum != "sha256:production" ||
		line.Approved.URL == "" || line.Production.URL == "" {
		t.Fatalf("line %+v", line)
	}
	if !line.Approved.ExpiresAt.Equal(start.Add(application.OrderArtworkTTL)) || !line.Production.ExpiresAt.Equal(start.Add(application.OrderArtworkTTL)) {
		t.Fatalf("expires approved=%s production=%s", line.Approved.ExpiresAt, line.Production.ExpiresAt)
	}
	if got, err := store.Fetch(line.Approved.URL); err != nil || string(got) != "approved-bytes" {
		t.Fatalf("approved fetch %q err=%v", got, err)
	}
	if got, err := store.Fetch(line.Production.URL); err != nil || string(got) != "production-bytes" {
		t.Fatalf("production fetch %q err=%v", got, err)
	}
	if strings.Contains(body, "private/blobs") || strings.Contains(body, artKey) || strings.Contains(body, prodKey) {
		t.Fatalf("storage key in body %s", body)
	}
	assertAdminAudit(t, db, orderFull, adminID, start, []string{ports.ArtworkKindApproved, ports.ArtworkKindProduction})

	clock.Advance(application.OrderArtworkTTL + time.Second)
	if _, err := store.Fetch(line.Approved.URL); err == nil {
		t.Fatal("approved url still valid after ttl")
	}
	if _, err := store.Fetch(line.Production.URL); err == nil {
		t.Fatal("production url still valid after ttl")
	}

	status, body = artworkDo(h, "test-token:"+adminID+":admin", orderPreview)
	if status != http.StatusOK || !strings.Contains(body, `"production":null`) {
		t.Fatalf("preview status=%d body=%s", status, body)
	}
	var preview application.OrderArtwork
	if err := json.Unmarshal([]byte(body), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Lines) != 1 || preview.Lines[0].Production != nil || preview.Lines[0].Approved.Checksum != "sha256:preview" {
		t.Fatalf("preview %+v", preview)
	}
	if got, err := store.Fetch(preview.Lines[0].Approved.URL); err != nil || string(got) != "preview-only" {
		t.Fatalf("preview fetch %q err=%v", got, err)
	}
	if strings.Contains(body, "private/blobs") || strings.Contains(body, prevKey) {
		t.Fatalf("storage key in preview %s", body)
	}
	assertAdminAudit(t, db, orderPreview, adminID, clock.Now(), []string{ports.ArtworkKindApproved})

	status, body = artworkDo(h, "test-token:"+adminID+":admin", orderMissing)
	if status != http.StatusNotFound || !strings.Contains(body, "plugin.metaldogs.order_not_found") {
		t.Fatalf("missing status=%d body=%s", status, body)
	}
	if n := countAudit(t, db, orderMissing); n != 0 {
		t.Fatalf("missing audit %d", n)
	}

	status, body = artworkDo(h, "test-token:"+adminID+":admin", orderEmpty)
	if status != http.StatusOK {
		t.Fatalf("empty status=%d body=%s", status, body)
	}
	var empty application.OrderArtwork
	if err := json.Unmarshal([]byte(body), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.OrderID != orderEmpty || len(empty.Lines) != 0 || strings.Contains(body, "private/blobs") {
		t.Fatalf("empty %+v body=%s", empty, body)
	}
	if n := countAudit(t, db, orderEmpty); n != 0 {
		t.Fatalf("empty audit %d", n)
	}

	status, body = artworkDo(h, "test-token:"+adminID+":admin", "not-a-uuid")
	if status != http.StatusBadRequest || !strings.Contains(body, "plugin.metaldogs.invalid_order_id") {
		t.Fatalf("bad id status=%d body=%s", status, body)
	}

	if _, err := db.ExecContext(context.Background(), `ALTER TABLE metaldogs_artwork_access_log RENAME TO metaldogs_artwork_access_log_blocked`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `ALTER TABLE metaldogs_artwork_access_log_blocked RENAME TO metaldogs_artwork_access_log`)
	})
	before := countAuditTable(t, db, "metaldogs_artwork_access_log_blocked", orderFull)
	status, body = artworkDo(h, "test-token:"+adminID+":admin", orderFull)
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "plugin.metaldogs.artwork_unavailable") {
		t.Fatalf("audit failure status=%d body=%s", status, body)
	}
	if strings.Contains(body, "http") || strings.Contains(body, "private/blobs") || strings.Contains(body, artKey) {
		t.Fatalf("audit failure leaked %s", body)
	}
	if countAuditTable(t, db, "metaldogs_artwork_access_log_blocked", orderFull) != before {
		t.Fatal("audit failure wrote a row")
	}
}

func artworkAdminRoute(app *plugin.App) (plugin.AdminRoute, bool) {
	for _, route := range app.AdminRoutes() {
		if route.Pattern == metaldogs.OrderArtworkAdminRoute {
			return route, true
		}
	}
	return plugin.AdminRoute{}, false
}

func mountArtworkAdmin(t *testing.T, app *plugin.App) http.Handler {
	t.Helper()
	route, ok := artworkAdminRoute(app)
	if !ok {
		t.Fatal("order artwork route not registered")
	}
	app.FreezePermissionRegistry()
	rbac.BindRuntime(app.PermissionRegistry())
	t.Cleanup(rbac.UnbindRuntime)
	mux := http.NewServeMux()
	mux.Handle(route.Pattern, adminhttp.RequirePermission(route.Permission)(route.Handler))
	return storefront.AuthMiddleware(artworkTokenParser{})(adminhttp.AdminContextMiddleware()(mux))
}

type artworkTokenParser struct{}

func (artworkTokenParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	parts := strings.SplitN(token, ":", 3)
	if len(parts) != 3 || parts[0] != "test-token" {
		return identity.Identity{}, io.ErrUnexpectedEOF
	}
	role := identity.Role(parts[2])
	if !role.IsValid() {
		return identity.Identity{}, io.ErrUnexpectedEOF
	}
	return identity.NewIdentity(parts[1], role)
}

func artworkDo(h http.Handler, token, orderID string) (int, string) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/"+orderID+"/artwork", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func assertAdminAudit(t *testing.T, db *sql.DB, orderID, actor string, at time.Time, kinds []string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT variant_id, kind, actor, channel, requested_at,
			order_id::text || ' ' || variant_id || ' ' || kind || ' ' || actor || ' ' || channel
		FROM metaldogs_artwork_access_log
		WHERE order_id = $1
		ORDER BY kind
	`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var variant, kind, gotActor, channel, blob string
		var requested time.Time
		if err := rows.Scan(&variant, &kind, &gotActor, &channel, &requested, &blob); err != nil {
			t.Fatal(err)
		}
		if gotActor != actor || channel != ports.ArtworkChannelAdmin {
			t.Fatalf("actor=%s channel=%s", gotActor, channel)
		}
		if !requested.UTC().Equal(at) {
			t.Fatalf("requested_at %s want %s", requested, at)
		}
		if strings.Contains(blob, "http") || strings.Contains(blob, "private/blobs") {
			t.Fatalf("audit blob %q", blob)
		}
		got = append(got, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != strings.Join(kinds, ",") {
		t.Fatalf("kinds %v want %v", got, kinds)
	}
}
