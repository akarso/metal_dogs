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
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const (
	cartPagePath = "/cart"
	// One custom tee at MVP; a template-to-product mapping is X2.
	customTeeSlug = "mdogs-tee"
)

// Cart adds a quantity-one line to the storefront session cart.
type Cart struct {
	carts    *cartApp.Service
	products catalog.ProductRepository
	variants catalog.VariantRepository
}

// New returns a session cart port. Nil dependencies fail closed on use.
func New(carts *cartApp.Service, products catalog.ProductRepository, variants catalog.VariantRepository) ports.SessionCart {
	return &Cart{carts: carts, products: products, variants: variants}
}

// BelongsToCustomTee reports whether variantID is a variant of the custom tee.
func (c *Cart) BelongsToCustomTee(ctx context.Context, variantID string) (bool, error) {
	if c == nil || c.variants == nil || c.products == nil {
		return false, &ports.CartError{Code: ports.CartUnavailable}
	}
	variant, err := c.variants.FindByID(ctx, variantID)
	if err != nil {
		if clientCartMiss(err) {
			return false, nil
		}
		return false, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	if variant == nil {
		return false, nil
	}
	product, err := c.products.FindBySlug(ctx, customTeeSlug)
	if err != nil {
		return false, &ports.CartError{Code: ports.CartUnavailable, Err: err}
	}
	if product == nil {
		return false, nil
	}
	return variant.ProductID == product.ID, nil
}

// AddLine creates a guest cart when the session is empty and writes the design field.
func (c *Cart) AddLine(ctx context.Context, session ports.CartSession, line ports.DesignLine) (ports.CartSession, ports.CartTarget, error) {
	if c == nil || c.carts == nil {
		return "", ports.CartTarget{}, &ports.CartError{Code: ports.CartUnavailable}
	}
	current, created, err := c.loadOrCreate(ctx, session)
	if err != nil {
		return "", ports.CartTarget{}, err
	}
	updated, err := c.carts.AddItem(ctx, current.ID, "", line.VariantID, 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{
			FieldCode: ports.DesignIDFieldCode,
			Value:     line.DesignID,
		}},
		UpdatedBy: "guest",
	})
	if err != nil {
		if created {
			_ = c.carts.DiscardEmptyGuestCart(ctx, current.ID)
		}
		return "", ports.CartTarget{}, classifyCart(err)
	}
	return ports.CartSession(updated.ID), ports.CartTarget{Path: cartPagePath}, nil
}

func (c *Cart) loadOrCreate(ctx context.Context, session ports.CartSession) (*domaincart.Cart, bool, error) {
	if id := strings.TrimSpace(string(session)); id != "" {
		current, err := c.carts.GetCart(ctx, id, "")
		// AddItem rejects a cart that is not active, so that cookie is not a session cart.
		if err == nil && current != nil && current.IsActive() {
			return current, false, nil
		}
		if err != nil && !clientCartMiss(err) {
			return nil, false, classifyCart(err)
		}
	}
	created, err := c.carts.CreateCart(ctx, "", currencyFrom(ctx))
	if err != nil {
		return nil, false, classifyCart(err)
	}
	return created, true, nil
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
