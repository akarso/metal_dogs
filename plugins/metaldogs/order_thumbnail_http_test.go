package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
	"github.com/akarso/shopanda/plugins/metaldogs/thumbnail"
)

const (
	thumbSignKey = "0123456789abcdef0123456789abcdef"
	thumbBaseURL = "https://shop.example"
)

func TestOrderThumbnail_NotRegisteredWithoutFlagOrKey(t *testing.T) {
	t.Parallel()
	flagOff := testApp(t, true)
	flagOff.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	flagOff.Config.Plugins.Extra["metaldogs"]["public_base_url"] = thumbBaseURL
	off := initMetalDogs(t, flagOff)
	if route, ok := orderThumbnailRoute(flagOff); ok {
		t.Fatalf("flag off registered %+v", route.Pattern)
	}
	link, err := off.OrderThumbnailURL(context.Background(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "11111111-1111-4111-8111-111111111111")
	if err != nil || link != "" {
		t.Fatalf("flag off link %q err=%v", link, err)
	}

	noKey := testApp(t, true)
	noKey.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	noKey.Config.Plugins.Extra["metaldogs"]["public_base_url"] = thumbBaseURL
	bare := initMetalDogs(t, noKey)
	if route, ok := orderThumbnailRoute(noKey); ok {
		t.Fatalf("key absent registered %+v", route.Pattern)
	}
	link, err = bare.OrderThumbnailURL(context.Background(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "11111111-1111-4111-8111-111111111111")
	if err != nil || link != "" {
		t.Fatalf("key absent link %q err=%v", link, err)
	}
}

func TestOrderThumbnail_ShortKeyIsSettingsError(t *testing.T) {
	t.Parallel()
	app := testApp(t, true)
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	app.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = "short-key"
	reg := plugin.NewRegistry(app.Logger)
	reg.Register(pluginsdk.Adapt(metaldogs.New()))
	summary := reg.InitAll(app)
	if summary.Failed != 1 || summary.Initialized != 0 {
		t.Fatalf("summary %+v", summary)
	}
	err := reg.Entries()[0].Err
	if err == nil || !strings.Contains(err.Error(), "thumbnail_signing_key") || !strings.Contains(err.Error(), "32") || strings.Contains(err.Error(), "short-key") {
		t.Fatalf("err = %v", err)
	}
	if route, ok := orderThumbnailRoute(app); ok {
		t.Fatalf("short key registered %+v", route.Pattern)
	}
}

func TestOrderThumbnailHTTP(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_thumbnail")
	at := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	const (
		orderA    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		orderB    = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		variantA  = "11111111-1111-4111-8111-111111111111"
		variantB  = "22222222-2222-4222-8222-222222222222"
		variantC  = "33333333-3333-4333-8333-333333333333"
		variantD  = "44444444-4444-4444-8444-444444444444"
		designA   = "55555555-5555-4555-8555-555555555555"
		versionA  = "66666666-6666-4666-8666-666666666666"
		designB   = "77777777-7777-4777-8777-777777777777"
		versionB  = "88888888-8888-4888-8888-888888888888"
		artA      = "99999999-9999-4999-8999-999999999991"
		mockA     = "99999999-9999-4999-8999-999999999992"
		thumbA    = "99999999-9999-4999-8999-999999999993"
		artB      = "99999999-9999-4999-8999-999999999994"
		mockB     = "99999999-9999-4999-8999-999999999995"
		thumbB    = "99999999-9999-4999-8999-999999999996"
		artC      = "99999999-9999-4999-8999-999999999997"
		mockC     = "99999999-9999-4999-8999-999999999998"
		thumbC    = "99999999-9999-4999-8999-999999999999"
		artMiss   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa01"
		mockMiss  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa02"
		thumbMiss = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa03"
		artNone   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa04"
	)
	keyA := "private/blobs/metaldogs/thumbnail/2026/11/" + thumbA + ".png"
	keyB := "private/blobs/metaldogs/thumbnail/2026/11/" + thumbB + ".png"
	keyC := "private/blobs/metaldogs/thumbnail/2026/11/" + thumbC + ".png"
	keyMiss := "private/blobs/metaldogs/thumbnail/2026/11/" + thumbMiss + ".png"
	bytesA := []byte("thumbnail-bytes-a")
	bytesB := []byte("thumbnail-bytes-b")
	bytesC := []byte("thumbnail-bytes-c")
	store := &thumbMemStore{body: map[string][]byte{
		keyA: bytesA,
		keyB: bytesB,
		keyC: bytesC,
	}}
	insertArtworkAsset(t, db, artA, domain.AssetTypeArtwork, "private/blobs/metaldogs/artwork/2026/11/"+artA+".png", at)
	insertArtworkAsset(t, db, mockA, domain.AssetTypeMockup, "private/blobs/metaldogs/mockup/2026/11/"+mockA+".png", at)
	insertArtworkAsset(t, db, thumbA, domain.AssetTypeThumbnail, keyA, at)
	insertArtworkAsset(t, db, artB, domain.AssetTypeArtwork, "private/blobs/metaldogs/artwork/2026/11/"+artB+".png", at)
	insertArtworkAsset(t, db, mockB, domain.AssetTypeMockup, "private/blobs/metaldogs/mockup/2026/11/"+mockB+".png", at)
	insertArtworkAsset(t, db, thumbB, domain.AssetTypeThumbnail, keyB, at)
	insertArtworkAsset(t, db, artC, domain.AssetTypeArtwork, "private/blobs/metaldogs/artwork/2026/11/"+artC+".png", at)
	insertArtworkAsset(t, db, mockC, domain.AssetTypeMockup, "private/blobs/metaldogs/mockup/2026/11/"+mockC+".png", at)
	insertArtworkAsset(t, db, thumbC, domain.AssetTypeThumbnail, keyC, at)
	insertArtworkAsset(t, db, artMiss, domain.AssetTypeArtwork, "private/blobs/metaldogs/artwork/2026/11/"+artMiss+".png", at)
	insertArtworkAsset(t, db, mockMiss, domain.AssetTypeMockup, "private/blobs/metaldogs/mockup/2026/11/"+mockMiss+".png", at)
	insertArtworkAsset(t, db, thumbMiss, domain.AssetTypeThumbnail, keyMiss, at)
	insertArtworkAsset(t, db, artNone, domain.AssetTypeArtwork, "private/blobs/metaldogs/artwork/2026/11/"+artNone+".png", at)
	insertThumbLine(t, db, orderA, variantA, designA, versionA, artA, mockA, thumbA, at)
	insertThumbLine(t, db, orderA, variantB, designA, versionA, artB, mockB, thumbB, at)
	insertThumbLine(t, db, orderB, variantA, designB, versionB, artC, mockC, thumbC, at)
	insertThumbLine(t, db, orderA, variantC, designA, versionA, artMiss, mockMiss, thumbMiss, at)
	insertThumbLine(t, db, orderA, variantD, designA, versionA, artNone, "", "", at)

	logBuf := &bytes.Buffer{}
	app := testApp(t, true)
	app.Logger = logger.NewWithWriter(logBuf, "info")
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	app.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	app.Config.Plugins.Extra["metaldogs"]["public_base_url"] = thumbBaseURL
	p := initMetalDogs(t, app)
	p.SetAssetStore(store)
	route, ok := orderThumbnailRoute(app)
	if !ok || route.NoCache || route.Handler == nil {
		t.Fatalf("route %+v", route)
	}

	link, err := p.OrderThumbnailURL(context.Background(), orderA, variantA)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "shop.example" || parsed.RawQuery != "" {
		t.Fatalf("link %q", link)
	}
	token := strings.TrimPrefix(parsed.Path, "/api/customizer/order-thumbnails/")
	if token == parsed.Path || token == "" {
		t.Fatalf("link path %q", parsed.Path)
	}
	if _, _, ok := thumbnail.Verify(thumbSignKey, token); !ok {
		t.Fatalf("link token rejected")
	}

	secrets := []string{keyA, keyB, keyC, keyMiss, token, thumbSignKey, "private/blobs"}
	okRec := serveOrderThumbnail(t, route, token)
	if okRec.Code != http.StatusOK || !bytes.Equal(okRec.Body.Bytes(), bytesA) {
		t.Fatalf("status=%d body=%q", okRec.Code, okRec.Body.Bytes())
	}
	if okRec.Header().Get("Content-Type") != "image/png" ||
		okRec.Header().Get("Content-Disposition") != "inline" ||
		okRec.Header().Get("Cache-Control") != "private, max-age=86400" ||
		okRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %#v", okRec.Header())
	}
	assertThumbnailClear(t, okRec, logBuf.String(), secrets...)

	misses := []string{
		tamperThumbnail(token, thumbnail.Token(thumbSignKey, orderB, variantA)),
		tamperThumbnail(token, thumbnail.Token(thumbSignKey, orderA, variantB)),
		"not-a-token",
		token[:len(token)/2],
		thumbnail.Token(thumbSignKey, orderA, variantD),
		thumbnail.Token(thumbSignKey, orderA, variantC),
	}
	var wantBody []byte
	var wantHeader string
	for i, bad := range misses {
		rec := serveOrderThumbnail(t, route, bad)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("miss %d status=%d body=%q", i, rec.Code, rec.Body.Bytes())
		}
		if wantBody == nil {
			wantBody = append([]byte(nil), rec.Body.Bytes()...)
			wantHeader = rec.Header().Get("Content-Type") + "|" + rec.Header().Get("Cache-Control") + "|" + rec.Header().Get("X-Content-Type-Options") + "|" + rec.Header().Get("Content-Disposition")
			if wantHeader != "application/json|no-store|nosniff|" {
				t.Fatalf("404 header %q", wantHeader)
			}
		}
		gotHeader := rec.Header().Get("Content-Type") + "|" + rec.Header().Get("Cache-Control") + "|" + rec.Header().Get("X-Content-Type-Options") + "|" + rec.Header().Get("Content-Disposition")
		if !bytes.Equal(rec.Body.Bytes(), wantBody) || gotHeader != wantHeader || rec.Header().Get("Content-Disposition") != "" {
			t.Fatalf("miss %d body=%q header=%q", i, rec.Body.Bytes(), gotHeader)
		}
		if bytes.Equal(rec.Body.Bytes(), bytesB) || bytes.Equal(rec.Body.Bytes(), bytesC) {
			t.Fatalf("miss %d returned another thumbnail", i)
		}
		assertThumbnailClear(t, rec, logBuf.String(), append(secrets, bad)...)
	}
	if len(wantBody) == 0 || !bytes.Equal(wantBody, []byte(`{"error":"not_found"}`)) {
		t.Fatalf("404 body %q", wantBody)
	}
	logText := logBuf.String()
	if strings.Count(logText, "metaldogs.order_thumbnail") != 1 ||
		!strings.Contains(logText, orderA) || !strings.Contains(logText, variantC) ||
		!strings.Contains(logText, `"cause":"object"`) {
		t.Fatalf("thumbnail fault log: %s", logText)
	}
	if strings.Contains(logText, keyMiss) || strings.Contains(logText, thumbSignKey) {
		t.Fatalf("fault log leaked: %s", logText)
	}

	empty, err := p.OrderThumbnailURL(context.Background(), orderA, variantD)
	if err != nil || empty != "" {
		t.Fatalf("no thumbnail link %q err=%v", empty, err)
	}

	noBase := testApp(t, true)
	noBase.Bootstrap = &plugin.Bootstrap{DB: db}
	noBase.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	noBase.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	bare := initMetalDogs(t, noBase)
	if _, err := bare.OrderThumbnailURL(context.Background(), orderA, variantA); err == nil || !strings.Contains(err.Error(), "public_base_url") {
		t.Fatalf("missing base err=%v", err)
	}
}

func tamperThumbnail(valid, other string) string {
	payload, _, _ := strings.Cut(other, ".")
	_, mac, _ := strings.Cut(valid, ".")
	return payload + "." + mac
}

func serveOrderThumbnail(t *testing.T, route plugin.PublicRoute, token string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(route.Pattern, route.Handler)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/customizer/order-thumbnails/"+token, nil))
	return rec
}

func orderThumbnailRoute(app *plugin.App) (plugin.PublicRoute, bool) {
	for _, route := range app.PublicRoutes() {
		if route.Pattern == metaldogs.OrderThumbnailRoute {
			return route, true
		}
	}
	return plugin.PublicRoute{}, false
}

func assertThumbnailClear(t *testing.T, rec *httptest.ResponseRecorder, log string, secrets ...string) {
	t.Helper()
	blob := rec.Body.String() + "\n" + log
	for k, vals := range rec.Header() {
		blob += "\n" + k
		for _, v := range vals {
			blob += "\n" + v
		}
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(blob, secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
}

func insertThumbLine(t *testing.T, db *sql.DB, orderID, variantID, designID, versionID, artID, mockID, thumbID string, at time.Time) {
	t.Helper()
	snap, err := domain.NewOrderDesignSnapshot(
		orderID, variantID, designID, versionID, "TEE-BLK-M",
		"black-tee", 3, map[string]string{"dog_name": "Burek"},
		"openai", "gpt-image", "prompt-v7",
		artID, "sha256:"+artID, "", "", at,
	)
	if err != nil {
		t.Fatal(err)
	}
	if mockID != "" {
		snap, err = snap.WithThumbnail(mockID, thumbID)
		if err != nil {
			t.Fatal(err)
		}
	}
	written, err := postgres.NewOrderDesignSnapshotRepo(db).Insert(context.Background(), snap)
	if err != nil || !written {
		t.Fatalf("snapshot written=%v err=%v", written, err)
	}
}

type thumbMemStore struct {
	body map[string][]byte
}

func (s *thumbMemStore) Put(context.Context, string, io.Reader, string) error {
	return nil
}

func (s *thumbMemStore) Read(_ context.Context, key string) ([]byte, error) {
	b, ok := s.body[key]
	if !ok {
		return nil, errors.New("absent")
	}
	return append([]byte(nil), b...), nil
}

func (s *thumbMemStore) Head(ctx context.Context, key string) error {
	_, err := s.Read(ctx, key)
	return err
}

func (s *thumbMemStore) SignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("unused")
}

func (s *thumbMemStore) Delete(context.Context, string) error { return nil }

var _ ports.AssetStore = (*thumbMemStore)(nil)
