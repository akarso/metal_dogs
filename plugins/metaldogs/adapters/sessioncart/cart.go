package sessioncart

import (
	"context"
	"errors"
	"strings"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const (
	cartPagePath = "/cart"
	// One custom tee at MVP; a template-to-product mapping is X2.
	customTeeSlug = "mdogs-tee"
)

type cartStore interface {
	GetCart(ctx context.Context, cartID, customerID string) (*domaincart.Cart, error)
	CreateCart(ctx context.Context, customerID, currency string) (*domaincart.Cart, error)
	AddItem(ctx context.Context, cartID, customerID, variantID string, quantity int, opts cartApp.AddItemOptions) (*domaincart.Cart, error)
	DiscardEmptyGuestCart(ctx context.Context, cartID string) error
}

type lineValues interface {
	List(ctx context.Context, target domainext.Target, includePrivate bool) ([]domainext.Value, error)
	UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error)
	DeleteAllForTarget(ctx context.Context, target domainext.Target) error
}

type cartLookup interface {
	FindByID(ctx context.Context, id string) (*domaincart.Cart, error)
}

// Cart adds a quantity-one line to the storefront session cart.
type Cart struct {
	carts    cartStore
	products catalog.ProductRepository
	variants catalog.VariantRepository
	lines    lineValues
	saved    cartLookup
	grant    func(context.Context) (context.Context, error)
}

// New returns a session cart port. Nil dependencies fail closed on use.
// grant is invoked only for the cart write and must name the fixed design field.
// saved reads a cart without the customer check the cart service applies to GetCart.
func New(carts *cartApp.Service, products catalog.ProductRepository, variants catalog.VariantRepository, lines lineValues, saved cartLookup, grant func(context.Context) (context.Context, error)) ports.SessionCart {
	return newCart(carts, products, variants, lines, saved, grant)
}

func newCart(carts cartStore, products catalog.ProductRepository, variants catalog.VariantRepository, lines lineValues, saved cartLookup, grant func(context.Context) (context.Context, error)) *Cart {
	return &Cart{carts: carts, products: products, variants: variants, lines: lines, saved: saved, grant: grant}
}

// BelongsToCustomTee reports whether variantID is a variant of the custom tee.
func (c *Cart) BelongsToCustomTee(ctx context.Context, variantID string) (bool, error) {
	lines, err := c.CustomTeeLines(ctx, []string{variantID})
	if err != nil {
		return false, err
	}
	_, ok := lines[strings.TrimSpace(variantID)]
	return ok, nil
}

// CustomTeeLines reports which variantIDs belong to the custom tee.
func (c *Cart) CustomTeeLines(ctx context.Context, variantIDs []string) (map[string]struct{}, error) {
	if c == nil || c.variants == nil || c.products == nil {
		return nil, &ports.CartError{Code: ports.CartUnavailable}
	}
	out := map[string]struct{}{}
	if len(variantIDs) == 0 {
		return out, nil
	}
	// A failed product read cannot show that every line is some other product.
	product, err := c.products.FindBySlug(ctx, customTeeSlug)
	if err != nil {
		return nil, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	if product == nil {
		return out, nil
	}
	seen := make(map[string]struct{}, len(variantIDs))
	for _, variantID := range variantIDs {
		variantID = strings.TrimSpace(variantID)
		if variantID == "" {
			continue
		}
		if _, ok := seen[variantID]; ok {
			continue
		}
		seen[variantID] = struct{}{}
		variant, err := c.variants.FindByID(ctx, variantID)
		if err != nil {
			if clientCartMiss(err) {
				continue
			}
			return nil, &ports.CartError{Code: ports.CartUnavailable, Err: err}
		}
		if variant != nil && variant.ProductID == product.ID {
			out[variantID] = struct{}{}
		}
	}
	return out, nil
}

// PersistedLineQuantities reads stored line quantities without an owner check.
func (c *Cart) PersistedLineQuantities(ctx context.Context, cartID string) (map[string]int, bool, error) {
	if c == nil || c.saved == nil {
		return nil, false, &ports.CartError{Code: ports.CartUnavailable}
	}
	cartID = strings.TrimSpace(cartID)
	if cartID == "" {
		return map[string]int{}, false, nil
	}
	current, err := c.saved.FindByID(ctx, cartID)
	if err != nil {
		return nil, false, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	if current == nil {
		return map[string]int{}, false, nil
	}
	qty := make(map[string]int, len(current.Items))
	for _, item := range current.Items {
		qty[item.VariantID] = item.Quantity
	}
	return qty, true, nil
}

// LineDesign reads the design id stored for a cart line, including a value written
// before the line itself is saved.
func (c *Cart) LineDesign(ctx context.Context, cartID, variantID string) (string, bool, error) {
	if c == nil || c.lines == nil {
		return "", false, &ports.CartError{Code: ports.CartUnavailable}
	}
	cartID = strings.TrimSpace(cartID)
	variantID = strings.TrimSpace(variantID)
	if cartID == "" || variantID == "" {
		return "", false, nil
	}
	values, err := c.lines.List(ctx, domainext.CartItemTarget(cartID, variantID), false)
	if err != nil {
		return "", false, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	for _, value := range values {
		if value.FieldCode != ports.DesignIDFieldCode || value.Payload.StringValue == nil {
			continue
		}
		id := strings.TrimSpace(*value.Payload.StringValue)
		if id == "" {
			return "", false, nil
		}
		return id, true, nil
	}
	return "", false, nil
}

// AddLine creates a guest cart when the session is empty and writes the design field.
func (c *Cart) AddLine(ctx context.Context, session ports.CartSession, line ports.DesignLine) (ports.CartSession, ports.CartTarget, error) {
	if c == nil || c.carts == nil {
		return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartUnavailable}
	}
	// GetCart and CreateCart must not carry the trusted-write marker.
	readCtx := pluginwrite.Strip(ctx)
	current, err := c.activeCart(readCtx, session)
	if err != nil {
		return "", ports.CartTarget{}, err
	}
	if current != nil {
		held, present, err := c.lineDesign(readCtx, current, line.VariantID)
		if err != nil {
			return "", ports.CartTarget{}, err
		}
		if present {
			if held == line.DesignID {
				return ports.CartSession(current.ID), ports.CartTarget{Path: cartPagePath}, nil
			}
			return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartConflict}
		}
	}
	if c.grant == nil {
		return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartUnavailable}
	}
	created := false
	if current == nil {
		current, err = c.carts.CreateCart(readCtx, "", currencyFrom(ctx))
		if err != nil {
			return "", ports.CartTarget{}, classifyCart(err)
		}
		created = true
	}
	writeCtx, err := c.grant(ctx)
	if err != nil {
		if created {
			_ = c.carts.DiscardEmptyGuestCart(readCtx, current.ID)
		}
		return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	if c.lines == nil {
		if created {
			_ = c.carts.DiscardEmptyGuestCart(readCtx, current.ID)
		}
		return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartUnavailable}
	}
	// cart.validate runs before AddItem stores extensions, so the field has to be there already.
	target := domainext.CartItemTarget(current.ID, line.VariantID)
	if _, err := c.lines.UpsertBatch(writeCtx, target, []domainext.ValueInput{{
		FieldCode: ports.DesignIDFieldCode,
		Value:     line.DesignID,
	}}, "guest", false); err != nil {
		if created {
			_ = c.carts.DiscardEmptyGuestCart(readCtx, current.ID)
		}
		return "", ports.CartTarget{}, classifyCart(err)
	}
	updated, err := c.carts.AddItem(writeCtx, current.ID, "", line.VariantID, 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{
			FieldCode: ports.DesignIDFieldCode,
			Value:     line.DesignID,
		}},
		UpdatedBy: "guest",
	})
	if err != nil {
		// AddItem can fail after the pre-write. A leftover value would pass validation on a later generic add.
		if delErr := c.lines.DeleteAllForTarget(writeCtx, target); delErr != nil {
			if created {
				_ = c.carts.DiscardEmptyGuestCart(readCtx, current.ID)
			}
			return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartUnavailable, Err: delErr}
		}
		if created {
			_ = c.carts.DiscardEmptyGuestCart(readCtx, current.ID)
		}
		return "", ports.CartTarget{}, classifyCart(err)
	}
	return ports.CartSession(updated.ID), ports.CartTarget{Path: cartPagePath}, nil
}

func (c *Cart) activeCart(ctx context.Context, session ports.CartSession) (*domaincart.Cart, error) {
	id := strings.TrimSpace(string(session))
	if id == "" {
		return nil, nil
	}
	current, err := c.carts.GetCart(ctx, id, "")
	// AddItem rejects a cart that is not active, so that cookie is not a session cart.
	if err == nil && current != nil && current.IsActive() {
		return current, nil
	}
	if err != nil && !clientCartMiss(err) {
		return nil, classifyCart(err)
	}
	return nil, nil
}

func (c *Cart) lineDesign(ctx context.Context, current *domaincart.Cart, variantID string) (string, bool, error) {
	present := false
	for _, item := range current.Items {
		if item.VariantID == variantID {
			present = true
			break
		}
	}
	if !present {
		return "", false, nil
	}
	if c.lines == nil {
		return "", false, &ports.CartError{Code: ports.CartUnavailable}
	}
	values, err := c.lines.List(ctx, domainext.CartItemTarget(current.ID, variantID), false)
	if err != nil {
		return "", false, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	for _, value := range values {
		if value.FieldCode != ports.DesignIDFieldCode || value.Payload.StringValue == nil {
			continue
		}
		return *value.Payload.StringValue, true, nil
	}
	return "", true, nil
}

func currencyFrom(ctx context.Context) string {
	if s := store.FromContext(ctx); s != nil && strings.TrimSpace(s.Currency) != "" {
		return s.Currency
	}
	return "EUR"
}

func classifyCart(err error) error {
	if err == nil {
		return nil
	}
	var ce *ports.CartError
	if errors.As(err, &ce) {
		return err
	}
	switch {
	case apperror.Is(err, apperror.CodeNotFound):
		return &ports.CartError{Code: ports.CartNotFound, Err: err}
	case apperror.Is(err, apperror.CodeValidation),
		apperror.Is(err, apperror.CodeCartValidationFailed),
		apperror.Is(err, apperror.CodeFieldValidationFailed),
		apperror.Is(err, apperror.CodeUnknownFieldCode):
		return &ports.CartError{Code: ports.CartUnprocessable, Err: err}
	case apperror.Is(err, apperror.CodeConflict):
		return &ports.CartError{Code: ports.CartConflict, Err: err}
	default:
		return &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
}

func clientCartMiss(err error) bool {
	return apperror.Is(err, apperror.CodeNotFound) ||
		apperror.Is(err, apperror.CodeForbidden) ||
		apperror.Is(err, apperror.CodeValidation)
}
