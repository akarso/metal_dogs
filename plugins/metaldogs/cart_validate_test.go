package metaldogs

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestCartFromValidatePayload_View(t *testing.T) {
	view := &extapi.CartView{ID: "c1", Items: []extapi.CartLineView{{VariantID: "v", Quantity: 2}}}
	got, err := cartFromValidatePayload(view)
	if err != nil || got == nil || got.ID != "c1" || len(got.Items) != 1 || got.Items[0].VariantID != "v" || got.Items[0].Quantity != 2 {
		t.Fatalf("view = %+v err=%v", got, err)
	}
	got, err = cartFromValidatePayload((*extapi.CartView)(nil))
	if err != nil || got != nil {
		t.Fatalf("nil view = %+v err=%v", got, err)
	}
}

func TestOnCartValidate_UnexpectedPayload(t *testing.T) {
	log := &recLog{}
	p := New()
	p.host = &surface{log: log}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", "not-a-cart")
	err := p.onCartValidate(hctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if log.errors != 1 {
		t.Fatalf("logged errors=%d", log.errors)
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_EmptyCartWithoutCatalog(t *testing.T) {
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &extapi.CartView{ID: "cart-1"})
	if err := New().onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_UnwiredCatalog(t *testing.T) {
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &extapi.CartView{ID: "cart-1", Items: []extapi.CartLineView{{VariantID: "variant-m", Quantity: 1}}})
	if err := New().onCartValidate(hctx); err == nil {
		t.Fatal("expected unwired catalog to fail closed")
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_OtherProductWithoutDesigns(t *testing.T) {
	catalog := &slugCatalog{slugs: map[string]string{"shirt": "other-shirt"}}
	cart := &qtyCart{}
	p := New()
	p.host = &surface{cart: cart, catalog: catalog}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &extapi.CartView{ID: "cart-1", Items: []extapi.CartLineView{{VariantID: "shirt", Quantity: 1}}})
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.ctx.Deadline(); !ok {
		t.Fatal("validation I/O has no deadline")
	}
	if catalog.calls != 1 || cart.getCalls != 0 {
		t.Fatalf("catalog=%d get=%d", catalog.calls, cart.getCalls)
	}
	if issues := hookIssues(hctx); len(issues) != 0 {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_EmptyProductSlugBlocks(t *testing.T) {
	catalog := &slugCatalog{slugs: map[string]string{"tee": ""}}
	p := New()
	p.host = &surface{cart: &qtyCart{}, catalog: catalog}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &extapi.CartView{ID: "cart-1", Items: []extapi.CartLineView{{VariantID: "tee", Quantity: 1}}})
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	issues := hookIssues(hctx)
	if len(issues) != 1 || issues[0].Code != ports.ProductUnavailableCode || issues[0].Message != "product is not available" || issues[0].VariantID != "tee" || issues[0].Level != "" {
		t.Fatalf("issues=%+v", issues)
	}
	if catalog.calls != 1 {
		t.Fatalf("catalog=%d", catalog.calls)
	}
}

func TestOnCartValidate_DraftTeeBlocks(t *testing.T) {
	catalog := &slugCatalog{hidden: map[string]string{"tee": customTeeSlug}}
	p := New()
	p.host = &surface{cart: &qtyCart{}, catalog: catalog}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", &extapi.CartView{ID: "cart-1", Items: []extapi.CartLineView{{VariantID: "tee", Quantity: 1}}})
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	issues := hookIssues(hctx)
	if len(issues) != 1 || issues[0].Code != ports.ProductUnavailableCode || issues[0].Message != "product is not available" || issues[0].VariantID != "tee" || issues[0].Level != "" {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestOnCartValidate_TeeWithoutDesignRepository(t *testing.T) {
	view := &extapi.CartView{ID: "cart-1", Items: []extapi.CartLineView{{VariantID: "tee", Quantity: 1}}}
	catalog := &slugCatalog{slugs: map[string]string{"tee": customTeeSlug}}
	cart := &qtyCart{missing: true}
	p := New()
	p.host = &surface{cart: cart, catalog: catalog}
	hctx := &extapi.HookContext{}
	hctx.Set("cart", view)
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.ctx.Deadline(); !ok {
		t.Fatal("validation I/O has no deadline")
	}
	if catalog.calls != 1 {
		t.Fatalf("catalog calls=%d", catalog.calls)
	}
	issues := hookIssues(hctx)
	if len(issues) != 1 || issues[0].Code != ports.DesignRequiredCode || issues[0].Message != "design required" || issues[0].VariantID != "tee" || issues[0].Level != "" {
		t.Fatalf("issues=%+v", issues)
	}

	hctx = &extapi.HookContext{}
	hctx.Set("cart", view)
	hctx.Set("pending_line", &extapi.PendingCartLine{
		VariantID: "tee",
		Quantity:  1,
		Extensions: []extapi.CartLineExtension{{
			Code:  ports.DesignIDFieldCode,
			Value: "design-a",
		}},
	})
	if err := p.onCartValidate(hctx); err != nil {
		t.Fatal(err)
	}
	issues = hookIssues(hctx)
	if len(issues) != 1 || issues[0].Code != ports.DesignNotFoundCode || issues[0].Message != "design not found" || issues[0].VariantID != "tee" || issues[0].Level != "" {
		t.Fatalf("issues=%+v", issues)
	}
}

type surface struct {
	cart       pluginsdk.CartPort
	catalog    pluginsdk.CatalogPort
	log        pluginsdk.Logger
	writeCalls int
}

func (s *surface) Cart() (pluginsdk.CartPort, bool) { return s.cart, s.cart != nil }
func (s *surface) Catalog() (pluginsdk.CatalogPort, bool) {
	return s.catalog, s.catalog != nil
}
func (s *surface) CatalogSeed() (pluginsdk.CatalogSeedPort, bool) { return nil, false }
func (s *surface) WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context {
	s.writeCalls++
	return pluginwrite.Grant(ctx, pluginName, fieldCodes...)
}
func (s *surface) Logger() pluginsdk.Logger { return s.log }

type recLog struct{ errors int }

func (r *recLog) Info(string, map[string]interface{})         {}
func (r *recLog) Warn(string, map[string]interface{})         {}
func (r *recLog) Error(string, error, map[string]interface{}) { r.errors++ }

type slugCatalog struct {
	slugs  map[string]string
	hidden map[string]string
	calls  int
	ctx    context.Context
}

func (s *slugCatalog) FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error) {
	found, err := s.FindVariants(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	return found[id], nil
}

func (s *slugCatalog) FindVariants(ctx context.Context, ids []string) (map[string]*extapi.CatalogVariant, error) {
	s.calls++
	s.ctx = ctx
	out := map[string]*extapi.CatalogVariant{}
	for _, id := range ids {
		if slug, ok := s.hidden[id]; ok {
			out[id] = &extapi.CatalogVariant{ID: id, ProductSlug: slug, ProductVisible: false}
			continue
		}
		slug, ok := s.slugs[id]
		if !ok {
			continue
		}
		out[id] = &extapi.CatalogVariant{ID: id, ProductSlug: slug, ProductVisible: strings.TrimSpace(slug) != ""}
	}
	return out, nil
}

func (s *slugCatalog) FindProduct(context.Context, string) (*extapi.CatalogProduct, error) {
	return nil, nil
}

type qtyCart struct {
	missing      bool
	persisted    *extapi.CartView
	getCalls     int
	addCalls     int
	upsertCalls  int
	deleteCalls  int
	ensureCalls  int
	discardCalls int
	discardID    string
	ensureCtx    context.Context
	addCtx       context.Context
	ensureView   *extapi.CartView
	lastOpts     pluginsdk.CartAddItemOpts
	addErr       error
}

func (q *qtyCart) AddItem(ctx context.Context, _, _, _ string, _ int, opts pluginsdk.CartAddItemOpts) error {
	q.addCalls++
	q.addCtx = ctx
	q.lastOpts = opts
	return q.addErr
}

func (q *qtyCart) Get(context.Context, string, string) (*extapi.CartView, error) {
	q.getCalls++
	if q.missing {
		return nil, apperror.NotFound("cart not found")
	}
	return q.persisted, nil
}

func (q *qtyCart) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return q.ensureView, nil
}

func (q *qtyCart) EnsureStorefrontCart(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ string) (*extapi.CartView, error) {
	q.ensureCalls++
	q.ensureCtx = ctx
	if q.ensureView != nil {
		return q.ensureView, nil
	}
	return &extapi.CartView{ID: "cart-1"}, nil
}

func (q *qtyCart) DeleteLineExtension(context.Context, string, string, string, string) error {
	q.deleteCalls++
	return nil
}

func (q *qtyCart) DiscardEmptyGuestCart(_ context.Context, _ http.ResponseWriter, _ *http.Request, cartID string) error {
	q.discardCalls++
	q.discardID = cartID
	return nil
}
func (q *qtyCart) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	q.upsertCalls++
	return nil
}

func hookIssues(hctx *extapi.HookContext) []extapi.CartValidationIssue {
	raw, _ := hctx.Get("validation_errors")
	issues, _ := raw.(*[]extapi.CartValidationIssue)
	if issues == nil {
		return nil
	}
	return *issues
}

func TestDesignCartIssue(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	variant := "variant-m"
	approved := domain.Design{Status: domain.DesignApproved, ExpiresAt: now.Add(time.Hour)}

	cases := []struct {
		name    string
		id      string
		present bool
		design  domain.Design
		found   bool
		code    string
		message string
	}{
		{name: "missing", present: false, code: ports.DesignRequiredCode, message: "design required"},
		{name: "blank", id: "  ", present: true, code: ports.DesignRequiredCode, message: "design required"},
		{name: "approved", id: "design-a", present: true, design: approved, found: true},
		{name: "not approved", id: "design-a", present: true, design: domain.Design{Status: domain.DesignOrdered, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignNotApprovedCode, message: "design is not approved"},
		{name: "expired status", id: "design-a", present: true, design: domain.Design{Status: domain.DesignExpired, ExpiresAt: now.Add(time.Hour)}, found: true, code: ports.DesignExpiredCode, message: "design has expired"},
		{name: "expired clock", id: "design-a", present: true, design: domain.Design{Status: domain.DesignApproved, ExpiresAt: now.Add(-time.Minute)}, found: true, code: ports.DesignExpiredCode, message: "design has expired"},
		{name: "unknown", id: "design-a", present: true, found: false, code: ports.DesignNotFoundCode, message: "design not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue, blocked := designCartIssue(now, variant, tc.id, tc.present, tc.design, tc.found)
			if tc.code == "" {
				if blocked {
					t.Fatalf("issue=%+v", issue)
				}
				return
			}
			if !blocked || issue.Code != tc.code || issue.Message != tc.message || issue.VariantID != variant || issue.Level != "" {
				t.Fatalf("issue=%+v blocked=%v", issue, blocked)
			}
		})
	}
}

func TestTeeLinesToValidate(t *testing.T) {
	tees := map[string]struct{}{"m": {}, "l": {}}
	stored := map[string]int{"m": 1, "l": 1}
	proposed := []extapi.CartLineView{{VariantID: "m", Quantity: 1}, {VariantID: "l", Quantity: 1}}

	if got := teeLinesToValidate(proposed, stored, true, tees); len(got) != 2 {
		t.Fatalf("read path = %v", got)
	}
	removed := []extapi.CartLineView{{VariantID: "l", Quantity: 1}}
	if got := teeLinesToValidate(removed, stored, true, tees); len(got) != 0 {
		t.Fatalf("unchanged stale line = %v", got)
	}
	changed := []extapi.CartLineView{{VariantID: "m", Quantity: 2}, {VariantID: "l", Quantity: 1}}
	got := teeLinesToValidate(changed, stored, true, tees)
	if len(got) != 1 {
		t.Fatalf("changed line = %v", got)
	}
	if _, ok := got["m"]; !ok {
		t.Fatalf("changed line = %v", got)
	}
	added := []extapi.CartLineView{{VariantID: "m", Quantity: 1}, {VariantID: "l", Quantity: 1}, {VariantID: "x", Quantity: 1}}
	if got := teeLinesToValidate(added, stored, true, tees); len(got) != 0 {
		t.Fatalf("other product = %v", got)
	}
	bare := []extapi.CartLineView{{VariantID: "m", Quantity: 1}}
	got = teeLinesToValidate(bare, map[string]int{"shirt": 1}, true, map[string]struct{}{"m": {}})
	if len(got) != 1 {
		t.Fatalf("new tee = %v", got)
	}
	if _, ok := got["m"]; !ok {
		t.Fatalf("new tee = %v", got)
	}
	if got := teeLinesToValidate(proposed, nil, false, tees); len(got) != 2 {
		t.Fatalf("missing cart = %v", got)
	}
}
