package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/hostorders"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOrderArtworkCommand_RegisteredOnlyWhenFlagOn(t *testing.T) {
	t.Parallel()
	off := testApp(t, true)
	regOff := plugin.NewRegistry(off.Logger)
	pOff := metaldogs.New()
	regOff.Register(pluginsdk.Adapt(pOff))
	if summary := regOff.InitAll(off); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	if _, ok := regOff.CLIRegistry().Get(metaldogs.CommandOrderArtwork); ok {
		t.Fatal("registered while order_snapshot.enabled is false")
	}
	err := pOff.RunOrderArtwork(pluginsdk.CLIContext{}, []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"})
	if err == nil || !strings.Contains(err.Error(), "order_snapshot.enabled") {
		t.Fatalf("flag off: %v", err)
	}

	on := testApp(t, true)
	on.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	regOn := plugin.NewRegistry(on.Logger)
	regOn.Register(pluginsdk.Adapt(metaldogs.New()))
	if summary := regOn.InitAll(on); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	cmd, ok := regOn.CLIRegistry().Get(metaldogs.CommandOrderArtwork)
	if !ok || !cmd.RequiresDB {
		t.Fatalf("flag on cli %+v", cmd)
	}
}

func TestOrderArtworkCLI_OrdersBoundAfterInit(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_artwork_late_port")
	const orderID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	app := testApp(t, true)
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	reg := plugin.NewRegistry(app.Logger)
	p := metaldogs.New()
	reg.Register(pluginsdk.Adapt(p))
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll %+v", summary)
	}
	if err := app.BindOrdersPort(pluginsdk.NewOrdersPort(foundOrderRepo{idleOrderRepo: idleOrderRepo{}, id: orderID})); err != nil {
		t.Fatal(err)
	}
	p.SetAssetStore(&expiringStore{clock: testutil.NewFakeClock(time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC))})
	out, err := runOrderArtwork(t, reg, &artworkCaptureLog{}, orderID)
	if err != nil {
		t.Fatal(err)
	}
	var view application.OrderArtwork
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.OrderID != orderID || len(view.Lines) != 0 {
		t.Fatalf("view %+v", view)
	}
}

type foundOrderRepo struct {
	idleOrderRepo
	id string
}

func (r foundOrderRepo) FindByID(_ context.Context, id string) (*domainorder.Order, error) {
	if id != r.id {
		return nil, nil
	}
	return &domainorder.Order{ID: id}, nil
}

func TestOrderArtworkCLI(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_artwork")
	ctx := context.Background()
	start := time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC)
	clock := testutil.NewFakeClock(start)

	const (
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
	artBody := []byte("approved-bytes")
	prodBody := []byte("production-bytes")
	prevBody := []byte("preview-only")

	store := &expiringStore{clock: clock, body: map[string][]byte{
		artKey:  artBody,
		prodKey: prodBody,
		prevKey: prevBody,
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
	log := &artworkCaptureLog{}

	out, err := runOrderArtwork(t, reg, log, orderFull)
	if err != nil {
		t.Fatal(err)
	}
	var full application.OrderArtwork
	if err := json.Unmarshal([]byte(out), &full); err != nil {
		t.Fatal(err)
	}
	if full.OrderID != orderFull || len(full.Lines) != 1 {
		t.Fatalf("view %+v", full)
	}
	line := full.Lines[0]
	if line.VariantID != variantFull || line.SKU != "TEE-BLK-M" || line.TemplateSlug != "black-tee" || line.TemplateVersion != 3 ||
		line.Texts["dog_name"] != "Burek" || line.Provider != "openai" || line.Model != "gpt-image" || line.PromptVersion != "prompt-v7" ||
		line.Approved.Checksum != "sha256:approved" || line.Production == nil || line.Production.Checksum != "sha256:production" {
		t.Fatalf("line %+v", line)
	}
	if !line.Approved.ExpiresAt.Equal(start.Add(application.OrderArtworkTTL)) || !line.Production.ExpiresAt.Equal(start.Add(application.OrderArtworkTTL)) {
		t.Fatalf("expires approved=%s production=%s", line.Approved.ExpiresAt, line.Production.ExpiresAt)
	}
	if got, err := store.Fetch(line.Approved.URL); err != nil || !bytes.Equal(got, artBody) {
		t.Fatalf("approved fetch %q err=%v", got, err)
	}
	if got, err := store.Fetch(line.Production.URL); err != nil || !bytes.Equal(got, prodBody) {
		t.Fatalf("production fetch %q err=%v", got, err)
	}
	for _, ttl := range store.ttls {
		if ttl != application.OrderArtworkTTL {
			t.Fatalf("ttl %s", ttl)
		}
	}
	assertArtworkDisposition(t, store, artKey, prodKey)
	assertNoStorageKey(t, out, log.dump(), artKey, prodKey, prevKey)
	assertAuditRows(t, db, orderFull, start, []string{ports.ArtworkKindApproved, ports.ArtworkKindProduction})

	clock.Advance(application.OrderArtworkTTL + time.Second)
	if _, err := store.Fetch(line.Approved.URL); err == nil {
		t.Fatal("approved url still valid after ttl")
	}
	if _, err := store.Fetch(line.Production.URL); err == nil {
		t.Fatal("production url still valid after ttl")
	}

	previewOut, err := runOrderArtwork(t, reg, log, orderPreview)
	if err != nil {
		t.Fatal(err)
	}
	var preview application.OrderArtwork
	if err := json.Unmarshal([]byte(previewOut), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Lines) != 1 || preview.Lines[0].Production != nil || preview.Lines[0].Approved.Checksum != "sha256:preview" {
		t.Fatalf("preview %+v", preview)
	}
	if !strings.Contains(previewOut, `"production":null`) {
		t.Fatalf("preview json %s", previewOut)
	}
	if got, err := store.Fetch(preview.Lines[0].Approved.URL); err != nil || !bytes.Equal(got, prevBody) {
		t.Fatalf("preview fetch %q err=%v", got, err)
	}
	assertNoStorageKey(t, previewOut, log.dump(), artKey, prodKey, prevKey)
	assertAuditRows(t, db, orderPreview, clock.Now(), []string{ports.ArtworkKindApproved})

	if _, err := runOrderArtwork(t, reg, log, orderMissing); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing: %v", err)
	}
	if n := countAudit(t, db, orderMissing); n != 0 {
		t.Fatalf("missing order audit rows %d", n)
	}
	emptyOut, err := runOrderArtwork(t, reg, log, orderEmpty)
	if err != nil {
		t.Fatal(err)
	}
	var empty application.OrderArtwork
	if err := json.Unmarshal([]byte(emptyOut), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.OrderID != orderEmpty || len(empty.Lines) != 0 {
		t.Fatalf("empty %+v", empty)
	}
	if n := countAudit(t, db, orderEmpty); n != 0 {
		t.Fatalf("empty order audit rows %d", n)
	}
	if orders.findCalls < 4 {
		t.Fatalf("FindByID calls %d", orders.findCalls)
	}
	errBefore := log.errorCount()
	if _, err := runOrderArtwork(t, reg, log, "not-a-uuid"); err == nil || !strings.Contains(err.Error(), "not a uuid") {
		t.Fatalf("bad id: %v", err)
	}
	if log.errorCount() != errBefore {
		t.Fatal("invalid order id logged at error")
	}
	assertNoStorageKey(t, emptyOut, log.dump(), artKey, prodKey, prevKey)

	if _, err := db.ExecContext(ctx, `ALTER TABLE metaldogs_artwork_access_log RENAME TO metaldogs_artwork_access_log_blocked`); err != nil {
		t.Fatal(err)
	}
	before := countAuditTable(t, db, "metaldogs_artwork_access_log_blocked", orderFull)
	blocked, err := runOrderArtwork(t, reg, log, orderFull)
	if err == nil || !strings.Contains(err.Error(), "artwork access failed") {
		t.Fatalf("audit failure err=%v out=%q", err, blocked)
	}
	if strings.Contains(blocked, "http") || strings.Contains(blocked, "private/blobs") || strings.Contains(blocked, artKey) {
		t.Fatalf("audit failure returned %q", blocked)
	}
	if countAuditTable(t, db, "metaldogs_artwork_access_log_blocked", orderFull) != before {
		t.Fatal("audit failure wrote a row")
	}
	assertNoStorageKey(t, blocked, log.dump(), artKey, prodKey, prevKey)
	if _, err := db.ExecContext(ctx, `ALTER TABLE metaldogs_artwork_access_log_blocked RENAME TO metaldogs_artwork_access_log`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE metaldogs_artwork_access_log SET actor = 'tampered' WHERE order_id = $1`, orderFull); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM metaldogs_artwork_access_log WHERE order_id = $1`, orderFull); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete: %v", err)
	}
}

func runOrderArtwork(t *testing.T, reg *plugin.Registry, log *artworkCaptureLog, orderID string) (string, error) {
	t.Helper()
	cmd, ok := reg.CLIRegistry().Get(metaldogs.CommandOrderArtwork)
	if !ok {
		t.Fatal("metaldogs:order-artwork not registered")
	}
	var buf bytes.Buffer
	metaldogs.SetOrderArtworkOutput(&buf)
	defer metaldogs.SetOrderArtworkOutput(nil)
	err := cmd.Run(cli.Context{Ctx: context.Background(), Logger: log}, []string{orderID})
	return buf.String(), err
}

func insertArtworkAsset(t *testing.T, db *sql.DB, id, kind, key string, at time.Time) {
	t.Helper()
	if err := postgres.NewAssetRepo(db).Insert(context.Background(), domain.Asset{
		ID: id, Type: kind, StorageKey: key,
		MimeType: "image/png", SizeBytes: 4, Width: 8, Height: 8,
		Checksum: "sha256:" + id, CreatedAt: at, DeleteAfter: at.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

func insertArtworkSnapshot(t *testing.T, db *sql.DB, orderID, variantID, designID, versionID, artID, artSum, prodID, prodSum string, at time.Time) {
	t.Helper()
	snap, err := domain.NewOrderDesignSnapshot(
		orderID, variantID, designID, versionID, "TEE-BLK-M",
		"black-tee", 3, map[string]string{"dog_name": "Burek"},
		"openai", "gpt-image", "prompt-v7",
		artID, artSum, prodID, prodSum, at,
	)
	if err != nil {
		t.Fatal(err)
	}
	written, err := postgres.NewOrderDesignSnapshotRepo(db).Insert(context.Background(), snap)
	if err != nil || !written {
		t.Fatalf("snapshot written=%v err=%v", written, err)
	}
}

func assertAuditRows(t *testing.T, db *sql.DB, orderID string, at time.Time, kinds []string) {
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
		var variant, kind, actor, channel, blob string
		var requested time.Time
		if err := rows.Scan(&variant, &kind, &actor, &channel, &requested, &blob); err != nil {
			t.Fatal(err)
		}
		if actor != application.ArtworkActorCLI || channel != ports.ArtworkChannelCLI {
			t.Fatalf("actor=%s channel=%s", actor, channel)
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
	var leaked int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'metaldogs_artwork_access_log'
		  AND column_name IN ('url', 'storage_key', 'signature')
	`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("leaking columns %d", leaked)
	}
}

func countAudit(t *testing.T, db *sql.DB, orderID string) int {
	t.Helper()
	return countAuditTable(t, db, "metaldogs_artwork_access_log", orderID)
}

func countAuditTable(t *testing.T, db *sql.DB, table, orderID string) int {
	t.Helper()
	var n int
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE order_id = $1`, table)
	if err := db.QueryRowContext(context.Background(), q, orderID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertNoStorageKey(t *testing.T, parts ...string) {
	t.Helper()
	keys := parts[len(parts)-3:]
	body := strings.Join(parts[:len(parts)-3], "\n")
	if strings.Contains(body, "bad-log-fields") || strings.Contains(body, "private/blobs") {
		t.Fatalf("storage key leaked in %q", body)
	}
	for _, key := range keys {
		if strings.Contains(body, key) {
			t.Fatalf("storage key leaked in %q", body)
		}
	}
}

type artworkOrders struct {
	ids       map[string]struct{}
	findCalls int
}

func (o *artworkOrders) FindByID(_ context.Context, id string) (*extapi.OrderView, error) {
	o.findCalls++
	if _, ok := o.ids[id]; !ok {
		return nil, nil
	}
	return &extapi.OrderView{ID: id}, nil
}

func (o *artworkOrders) Lines(context.Context, string) ([]extapi.OrderLineView, error) {
	return nil, nil
}

func (o *artworkOrders) LinesForOrders(context.Context, []string) (map[string][]extapi.OrderLineView, error) {
	return map[string][]extapi.OrderLineView{}, nil
}

func (o *artworkOrders) ListCreatedSince(context.Context, time.Time, string, int) ([]extapi.OrderView, error) {
	return nil, nil
}

type expiringToken struct {
	key string
	exp time.Time
}

type issuedSign struct {
	key  string
	disp ports.SignedDisposition
}

type expiringStore struct {
	clock *testutil.FakeClock
	body  map[string][]byte
	token map[string]expiringToken
	ttls  []time.Duration
	signs []issuedSign
	mu    sync.Mutex
}

func (s *expiringStore) Put(context.Context, string, io.Reader, string) error {
	return fmt.Errorf("unused")
}
func (s *expiringStore) Read(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("unused")
}
func (s *expiringStore) Head(context.Context, string) error { return fmt.Errorf("unused") }
func (s *expiringStore) Delete(context.Context, string) error {
	return fmt.Errorf("unused")
}

func (s *expiringStore) SignedGetURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	return s.issue(key, ttl, "")
}

func (s *expiringStore) SignedGetURLDisposition(_ context.Context, key string, ttl time.Duration, disp ports.SignedDisposition) (string, error) {
	if disp != ports.SignedDispositionAttachment && disp != ports.SignedDispositionInline {
		return "", fmt.Errorf("disposition required")
	}
	return s.issue(key, ttl, disp)
}

func (s *expiringStore) issue(key string, ttl time.Duration, disp ports.SignedDisposition) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.body[key]; !ok {
		return "", fmt.Errorf("missing blob")
	}
	if s.token == nil {
		s.token = map[string]expiringToken{}
	}
	s.ttls = append(s.ttls, ttl)
	s.signs = append(s.signs, issuedSign{key: key, disp: disp})
	id := fmt.Sprintf("u%d", len(s.ttls))
	s.token[id] = expiringToken{key: key, exp: s.clock.Now().Add(ttl)}
	return "https://artwork.invalid/" + id, nil
}

func assertArtworkDisposition(t *testing.T, store *expiringStore, approvedKey, productionKey string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	var approved, production bool
	for _, sign := range store.signs {
		if sign.key == approvedKey && sign.disp == "" {
			approved = true
		}
		if sign.key == productionKey && sign.disp == ports.SignedDispositionAttachment {
			production = true
		}
	}
	if !approved || !production {
		t.Fatalf("signs %+v", store.signs)
	}
}

func (s *expiringStore) Fetch(url string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimPrefix(url, "https://artwork.invalid/")
	tok, ok := s.token[id]
	if !ok {
		return nil, fmt.Errorf("unknown url")
	}
	if !s.clock.Now().Before(tok.exp) {
		return nil, fmt.Errorf("expired")
	}
	return append([]byte(nil), s.body[tok.key]...), nil
}

type artworkCaptureLog struct {
	mu      sync.Mutex
	text    bytes.Buffer
	badKeys []string
	errors  int
}

func (l *artworkCaptureLog) Info(event string, fields map[string]interface{}) {
	l.record(event, nil, fields)
}

func (l *artworkCaptureLog) Warn(event string, fields map[string]interface{}) {
	l.record(event, nil, fields)
}

func (l *artworkCaptureLog) Error(event string, err error, fields map[string]interface{}) {
	l.record(event, err, fields)
}

func (l *artworkCaptureLog) errorCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.errors
}

func (l *artworkCaptureLog) record(event string, err error, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		l.errors++
	}
	fmt.Fprintf(&l.text, "%s\n", event)
	if err != nil {
		fmt.Fprintf(&l.text, "%s\n", err.Error())
	}
	for k, v := range fields {
		if k != "order_id" && k != "actor" {
			l.badKeys = append(l.badKeys, k)
		}
		fmt.Fprintf(&l.text, "%s=%v\n", k, v)
	}
}

func (l *artworkCaptureLog) dump() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.badKeys) > 0 {
		return l.text.String() + "\nbad-log-fields:" + strings.Join(l.badKeys, ",")
	}
	return l.text.String()
}
