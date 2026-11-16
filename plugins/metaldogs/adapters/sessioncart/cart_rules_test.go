package sessioncart

import (
	"context"
	"errors"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestAddLine_ReadContextHasNoWriteGrant(t *testing.T) {
	variant := "variant-m"
	design := "design-a"
	parent := pluginwrite.Grant(context.Background(), "metaldogs", ports.DesignIDFieldCode)

	t.Run("new cart", func(t *testing.T) {
		store := &recordingCarts{}
		lines := &recordingLines{}
		grants := 0
		cart := newCart(store, nil, nil, lines, nil, func(ctx context.Context) (context.Context, error) {
			grants++
			return pluginwrite.Grant(ctx, "metaldogs", ports.DesignIDFieldCode), nil
		})
		got, target, err := cart.AddLine(parent, "", ports.DesignLine{VariantID: variant, DesignID: design})
		if err != nil {
			t.Fatal(err)
		}
		if got == "" || target.Path != "/cart" {
			t.Fatalf("session=%q target=%+v", got, target)
		}
		if store.getCalls != 0 || store.createCalls != 1 || store.addCalls != 1 || lines.upsertCalls != 1 || grants != 1 {
			t.Fatalf("get=%d create=%d add=%d upsert=%d grants=%d", store.getCalls, store.createCalls, store.addCalls, lines.upsertCalls, grants)
		}
		if granted(store.createCtx) || !granted(store.addCtx) || !granted(lines.upsertCtx) {
			t.Fatalf("create granted=%v add granted=%v upsert granted=%v", granted(store.createCtx), granted(store.addCtx), granted(lines.upsertCtx))
		}
	})

	t.Run("existing line absent", func(t *testing.T) {
		current := mustCart(t, "cart-1", "")
		store := &recordingCarts{current: current}
		lines := &recordingLines{}
		grants := 0
		cart := newCart(store, nil, nil, lines, nil, func(ctx context.Context) (context.Context, error) {
			grants++
			return pluginwrite.Grant(ctx, "metaldogs", ports.DesignIDFieldCode), nil
		})
		if _, _, err := cart.AddLine(parent, ports.CartSession(current.ID), ports.DesignLine{VariantID: variant, DesignID: design}); err != nil {
			t.Fatal(err)
		}
		if store.getCalls != 1 || store.createCalls != 0 || store.addCalls != 1 || lines.calls != 0 || lines.upsertCalls != 1 || grants != 1 {
			t.Fatalf("get=%d create=%d add=%d list=%d upsert=%d grants=%d", store.getCalls, store.createCalls, store.addCalls, lines.calls, lines.upsertCalls, grants)
		}
		if granted(store.getCtx) || !granted(store.addCtx) || !granted(lines.upsertCtx) {
			t.Fatalf("get granted=%v add granted=%v upsert granted=%v", granted(store.getCtx), granted(store.addCtx), granted(lines.upsertCtx))
		}
	})

	t.Run("same design", func(t *testing.T) {
		current := mustCart(t, "cart-1", variant)
		store := &recordingCarts{current: current}
		lines := &recordingLines{design: design}
		grants := 0
		cart := newCart(store, nil, nil, lines, nil, func(ctx context.Context) (context.Context, error) {
			grants++
			return pluginwrite.Grant(ctx, "metaldogs", ports.DesignIDFieldCode), nil
		})
		got, _, err := cart.AddLine(parent, ports.CartSession(current.ID), ports.DesignLine{VariantID: variant, DesignID: design})
		if err != nil {
			t.Fatal(err)
		}
		if got != ports.CartSession(current.ID) {
			t.Fatalf("session=%q", got)
		}
		if store.addCalls != 0 || store.createCalls != 0 || grants != 0 || lines.calls != 1 || lines.upsertCalls != 0 {
			t.Fatalf("add=%d create=%d grants=%d list=%d upsert=%d", store.addCalls, store.createCalls, grants, lines.calls, lines.upsertCalls)
		}
		if granted(store.getCtx) || granted(lines.ctx) {
			t.Fatal("read context can write the design field")
		}
	})

	t.Run("other design", func(t *testing.T) {
		current := mustCart(t, "cart-1", variant)
		store := &recordingCarts{current: current}
		lines := &recordingLines{design: "design-b"}
		grants := 0
		cart := newCart(store, nil, nil, lines, nil, func(ctx context.Context) (context.Context, error) {
			grants++
			return pluginwrite.Grant(ctx, "metaldogs", ports.DesignIDFieldCode), nil
		})
		_, _, err := cart.AddLine(context.Background(), ports.CartSession(current.ID), ports.DesignLine{VariantID: variant, DesignID: design})
		cartErr, ok := err.(*ports.CartError)
		if !ok || cartErr.Code != ports.CartConflict {
			t.Fatalf("err=%v", err)
		}
		if store.addCalls != 0 || grants != 0 || lines.upsertCalls != 0 {
			t.Fatalf("add=%d grants=%d upsert=%d", store.addCalls, grants, lines.upsertCalls)
		}
		if granted(store.getCtx) {
			t.Fatal("GetCart context can write the design field")
		}
	})

	t.Run("line without design", func(t *testing.T) {
		current := mustCart(t, "cart-1", variant)
		store := &recordingCarts{current: current}
		lines := &recordingLines{}
		grants := 0
		cart := newCart(store, nil, nil, lines, nil, func(ctx context.Context) (context.Context, error) {
			grants++
			return ctx, nil
		})
		_, _, err := cart.AddLine(context.Background(), ports.CartSession(current.ID), ports.DesignLine{VariantID: variant, DesignID: design})
		cartErr, ok := err.(*ports.CartError)
		if !ok || cartErr.Code != ports.CartConflict {
			t.Fatalf("err=%v", err)
		}
		if store.addCalls != 0 || grants != 0 || lines.upsertCalls != 0 {
			t.Fatalf("add=%d grants=%d upsert=%d", store.addCalls, grants, lines.upsertCalls)
		}
	})
}

func TestAddLine_AddItemFailureDeletesDesignValue(t *testing.T) {
	variant := "variant-m"
	current := mustCart(t, "cart-1", "")
	store := &recordingCarts{current: current, addErr: errors.New("no stock")}
	lines := &recordingLines{}
	cart := newCart(store, nil, nil, lines, nil, func(ctx context.Context) (context.Context, error) {
		return pluginwrite.Grant(ctx, "metaldogs", ports.DesignIDFieldCode), nil
	})
	_, _, err := cart.AddLine(context.Background(), ports.CartSession(current.ID), ports.DesignLine{VariantID: variant, DesignID: "design-a"})
	if err == nil {
		t.Fatal("expected add failure")
	}
	if store.addCalls != 1 || store.discardCalls != 0 || lines.upsertCalls != 1 || lines.deleteCalls != 1 {
		t.Fatalf("add=%d discard=%d upsert=%d delete=%d", store.addCalls, store.discardCalls, lines.upsertCalls, lines.deleteCalls)
	}
	if !granted(lines.deleteCtx) || lines.deleteTarget != domainext.CartItemTarget(current.ID, variant) {
		t.Fatalf("delete granted=%v target=%+v", granted(lines.deleteCtx), lines.deleteTarget)
	}
}

func TestCustomTeeLines_LoadsProductOnce(t *testing.T) {
	products := &slugProducts{product: &catalog.Product{ID: "tee"}}
	variants := &idVariants{}
	cart := newCart(nil, products, variants, nil, nil, nil)
	got, err := cart.CustomTeeLines(context.Background(), []string{"m", "other", "m"})
	if err != nil {
		t.Fatal(err)
	}
	if products.calls != 1 || variants.calls != 2 {
		t.Fatalf("product calls=%d variant calls=%d", products.calls, variants.calls)
	}
	if _, ok := got["m"]; !ok || len(got) != 1 {
		t.Fatalf("lines=%v", got)
	}
}

func granted(ctx context.Context) bool {
	_, ok := pluginwrite.For(ctx, ports.DesignIDFieldCode)
	return ok
}

func mustCart(t *testing.T, id, variant string) *domaincart.Cart {
	t.Helper()
	current, err := domaincart.NewCart(id, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if variant != "" {
		if err := current.AddItem(variant, 2, shared.MustNewMoney(100, "EUR")); err != nil {
			t.Fatal(err)
		}
	}
	return &current
}

type recordingCarts struct {
	current      *domaincart.Cart
	getCalls     int
	createCalls  int
	addCalls     int
	discardCalls int
	addErr       error
	getCtx       context.Context
	createCtx    context.Context
	addCtx       context.Context
}

func (r *recordingCarts) GetCart(ctx context.Context, cartID, customerID string) (*domaincart.Cart, error) {
	r.getCalls++
	r.getCtx = ctx
	return r.current, nil
}

func (r *recordingCarts) CreateCart(ctx context.Context, customerID, currency string) (*domaincart.Cart, error) {
	r.createCalls++
	r.createCtx = ctx
	created, err := domaincart.NewCart("created", currency)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (r *recordingCarts) AddItem(ctx context.Context, cartID, customerID, variantID string, quantity int, opts cartApp.AddItemOptions) (*domaincart.Cart, error) {
	r.addCalls++
	r.addCtx = ctx
	if r.addErr != nil {
		return nil, r.addErr
	}
	created, err := domaincart.NewCart(cartID, "EUR")
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (r *recordingCarts) DiscardEmptyGuestCart(ctx context.Context, cartID string) error {
	r.discardCalls++
	return nil
}

type recordingLines struct {
	design       string
	calls        int
	upsertCalls  int
	deleteCalls  int
	ctx          context.Context
	upsertCtx    context.Context
	deleteCtx    context.Context
	deleteTarget domainext.Target
}

func (r *recordingLines) List(ctx context.Context, target domainext.Target, includePrivate bool) ([]domainext.Value, error) {
	r.calls++
	r.ctx = ctx
	if r.design == "" {
		return nil, nil
	}
	design := r.design
	return []domainext.Value{{
		FieldCode: ports.DesignIDFieldCode,
		Payload:   domainext.ValuePayload{StringValue: &design},
	}}, nil
}

func (r *recordingLines) UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error) {
	r.upsertCalls++
	r.upsertCtx = ctx
	return nil, nil
}

func (r *recordingLines) DeleteAllForTarget(ctx context.Context, target domainext.Target) error {
	r.deleteCalls++
	r.deleteCtx = ctx
	r.deleteTarget = target
	return nil
}

type slugProducts struct {
	catalog.ProductRepository
	calls   int
	product *catalog.Product
}

func (s *slugProducts) FindBySlug(context.Context, string) (*catalog.Product, error) {
	s.calls++
	return s.product, nil
}

type idVariants struct {
	catalog.VariantRepository
	calls int
}

func (v *idVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	v.calls++
	productID := "tee"
	if id == "other" {
		productID = "other"
	}
	return &catalog.Variant{ID: id, ProductID: productID}, nil
}
