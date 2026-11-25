package metaldogs_test

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOrderLineDisplay_NotRegisteredWhileDark(t *testing.T) {
	t.Parallel()
	flagOff := testApp(t, true)
	flagOff.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	flagOff.Config.Plugins.Extra["metaldogs"]["public_base_url"] = thumbBaseURL
	initMetalDogs(t, flagOff)
	if orderLineDisplayRegistered(flagOff) {
		t.Fatal("hook registered while order_snapshot.enabled is false")
	}

	noKey := testApp(t, true)
	noKey.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	noKey.Config.Plugins.Extra["metaldogs"]["public_base_url"] = thumbBaseURL
	initMetalDogs(t, noKey)
	if orderLineDisplayRegistered(noKey) {
		t.Fatal("hook registered while thumbnail_signing_key is absent")
	}

	noBase := testApp(t, true)
	noBase.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	noBase.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	initMetalDogs(t, noBase)
	if orderLineDisplayRegistered(noBase) {
		t.Fatal("hook registered while public_base_url is empty")
	}

	disabled := testApp(t, false)
	disabled.Config.Plugins.Extra = map[string]map[string]string{
		"metaldogs": {
			"order_snapshot.enabled": "true",
			"thumbnail_signing_key":  thumbSignKey,
			"public_base_url":        thumbBaseURL,
		},
	}
	disabled.SetHookRegistry(hooks.NewRegistry(disabled.Logger))
	if err := pluginsdk.Adapt(metaldogs.New()).Init(disabled); err == nil {
		t.Fatal("disabled plugin Init succeeded")
	}
	if orderLineDisplayRegistered(disabled) {
		t.Fatal("hook registered while the plugin is disabled")
	}
}

func TestOrderLineDisplay_HTTPBaseWarnsAndSkipsRegistration(t *testing.T) {
	t.Parallel()
	logBuf := &bytes.Buffer{}
	app := testApp(t, true)
	app.Logger = logger.NewWithWriter(logBuf, "info")
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	app.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	app.Config.Plugins.Extra["metaldogs"]["public_base_url"] = "http://shop.example"
	initMetalDogs(t, app)
	if orderLineDisplayRegistered(app) {
		t.Fatal("hook registered for a non-https public_base_url")
	}
	if _, ok := orderThumbnailRoute(app); !ok {
		t.Fatal("thumbnail route missing; http base still serves the endpoint")
	}
	if strings.Count(logBuf.String(), "metaldogs.order_line_display.public_base_url") != 1 ||
		!strings.Contains(logBuf.String(), `"reason":"not_https"`) {
		t.Fatalf("startup warning: %s", logBuf.String())
	}
}

func TestOrderLineDisplay_HookOnPostgres(t *testing.T) {
	db := testutil.OpenIsolated(t, "order_line_display")
	at := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	const (
		orderA   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
		variantA = "11111111-1111-4111-8111-111111111101"
		variantB = "22222222-2222-4222-8222-222222222202"
		variantC = "33333333-3333-4333-8333-333333333303"
		designA  = "55555555-5555-4555-8555-555555555501"
		versionA = "66666666-6666-4666-8666-666666666601"
		artA     = "99999999-9999-4999-8999-9999999999a1"
		mockA    = "99999999-9999-4999-8999-9999999999a2"
		thumbA   = "99999999-9999-4999-8999-9999999999a3"
		artNone  = "99999999-9999-4999-8999-9999999999a4"
	)
	keyA := "private/blobs/metaldogs/thumbnail/2026/11/" + thumbA + ".png"
	bodyA := []byte("thumbnail-bytes-line")
	store := &thumbMemStore{body: map[string][]byte{keyA: bodyA}}
	insertArtworkAsset(t, db, artA, "artwork", "private/blobs/metaldogs/artwork/2026/11/"+artA+".png", at)
	insertArtworkAsset(t, db, mockA, "mockup", "private/blobs/metaldogs/mockup/2026/11/"+mockA+".png", at)
	insertArtworkAsset(t, db, thumbA, "thumbnail", keyA, at)
	insertArtworkAsset(t, db, artNone, "artwork", "private/blobs/metaldogs/artwork/2026/11/"+artNone+".png", at)
	insertThumbLine(t, db, orderA, variantA, designA, versionA, artA, mockA, thumbA, at)
	insertThumbLine(t, db, orderA, variantB, designA, versionA, artNone, "", "", at)

	logBuf := &bytes.Buffer{}
	app := testApp(t, true)
	app.Logger = logger.NewWithWriter(logBuf, "info")
	app.Bootstrap = &plugin.Bootstrap{DB: db}
	app.Config.Plugins.Extra["metaldogs"]["order_snapshot.enabled"] = "true"
	app.Config.Plugins.Extra["metaldogs"]["thumbnail_signing_key"] = thumbSignKey
	app.Config.Plugins.Extra["metaldogs"]["public_base_url"] = thumbBaseURL
	p := initMetalDogs(t, app)
	p.SetAssetStore(store)
	if !orderLineDisplayRegistered(app) {
		t.Fatal("hook is not registered")
	}
	route, ok := orderThumbnailRoute(app)
	if !ok {
		t.Fatal("thumbnail route missing")
	}

	wantLink, err := p.OrderThumbnailURL(context.Background(), orderA, variantA)
	if err != nil || wantLink == "" {
		t.Fatalf("link %q err=%v", wantLink, err)
	}
	enLabel, enAlt := "Your design", "Preview of your custom design"
	cases := []struct {
		lang  string
		set   bool
		label string
		alt   string
	}{
		{lang: "en", set: true, label: enLabel, alt: enAlt},
		{lang: "pl", set: true, label: "Twój projekt", alt: "Podgląd Twojego projektu"},
		{lang: "de", set: true, label: "Dein Design", alt: "Vorschau deines individuellen Designs"},
		{lang: "fr", set: true, label: "Votre création", alt: "Aperçu de votre création personnalisée"},
		{lang: "es", set: true, label: "Tu diseño", alt: "Vista previa de tu diseño personalizado"},
		{lang: "pl-PL", set: true, label: "Twój projekt", alt: "Podgląd Twojego projektu"},
		{lang: "zz", set: true, label: enLabel, alt: enAlt},
		{lang: "", set: true, label: enLabel, alt: enAlt},
		{lang: "", set: false, label: enLabel, alt: enAlt},
	}
	for _, tc := range cases {
		got := invokeOrderLine(t, app, orderA, variantA, tc.lang, tc.set)
		if len(got) != 1 {
			t.Fatalf("lang %q set=%v fields=%+v", tc.lang, tc.set, got)
		}
		field := got[0]
		if field.Code != "metaldogs.thumbnail" || field.Kind != extapi.DisplayKindImage ||
			field.Value != wantLink || field.Label != tc.label || field.Alt != tc.alt {
			t.Fatalf("lang %q field=%+v", tc.lang, field)
		}
	}
	if strings.Contains(logBuf.String(), "metaldogs.order_line_display") {
		t.Fatalf("thumbnail line logged: %s", logBuf.String())
	}

	for _, variantID := range []string{variantB, variantC} {
		got := invokeOrderLine(t, app, orderA, variantID, "en", true)
		if len(got) != 0 {
			t.Fatalf("variant %s fields=%+v", variantID, got)
		}
		if !strings.Contains(logBuf.String(), orderA) || !strings.Contains(logBuf.String(), variantID) ||
			!strings.Contains(logBuf.String(), "metaldogs.order_line_display.skipped") {
			t.Fatalf("skip log missing %s: %s", variantID, logBuf.String())
		}
	}

	token := strings.TrimPrefix(wantLink, thumbBaseURL+"/api/customizer/order-thumbnails/")
	rec := serveOrderThumbnail(t, route, token)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), bodyA) {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.Bytes())
	}
	if strings.Contains(logBuf.String(), keyA) || strings.Contains(logBuf.String(), thumbSignKey) || strings.Contains(logBuf.String(), token) {
		t.Fatalf("log leaked: %s", logBuf.String())
	}

	dead, err := sql.Open("pgx", "postgres://shopanda:changeme@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	dead.Close()
	app.Bootstrap.DB = dead
	before := logBuf.Len()
	got := invokeOrderLine(t, app, orderA, variantA, "en", true)
	if len(got) != 0 {
		t.Fatalf("failed lookup fields=%+v", got)
	}
	failLog := logBuf.String()[before:]
	if !strings.Contains(failLog, "metaldogs.order_line_display.failed") ||
		!strings.Contains(failLog, orderA) || !strings.Contains(failLog, variantA) {
		t.Fatalf("fail log: %s", failLog)
	}
	if strings.Contains(failLog, keyA) || strings.Contains(failLog, thumbSignKey) || strings.Contains(failLog, "postgres://") {
		t.Fatalf("fail log leaked: %s", failLog)
	}
}

func invokeOrderLine(t *testing.T, app *plugin.App, orderID, variantID, lang string, setLang bool) []extapi.OrderLineDisplayField {
	t.Helper()
	hookCtx := hooks.NewContext(hooks.HookOrderLineDisplay)
	hookCtx.Set("order_id", orderID)
	hookCtx.Set("variant_id", variantID)
	if setLang {
		hookCtx.Set("language", lang)
	}
	fields := []extapi.OrderLineDisplayField{}
	hookCtx.Set("fields", &fields)
	var failed int
	app.HookRegistry().InvokeContinueOnError(context.Background(), hookCtx, func(string, error) {
		failed++
	})
	if failed != 0 {
		t.Fatalf("hook returned an error (%d)", failed)
	}
	return hooks.OrderLineDisplayFieldsFromContext(hookCtx)
}

func orderLineDisplayRegistered(app *plugin.App) bool {
	reg := app.HookRegistry()
	if reg == nil {
		return false
	}
	for _, entry := range reg.Catalog() {
		if entry.Name == string(extapi.HookOrderLineDisplay) && len(entry.Handlers) > 0 {
			return true
		}
	}
	return false
}
