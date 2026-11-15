package ports

import (
	"context"
	"net/http"
)

// DesignIDFieldCode is the cart-line extension written only by the add-to-cart route.
const DesignIDFieldCode = "metaldogs.design_id"

// DesignLine is one custom-tee line. Quantity is always one; the field code is not part of this value.
type DesignLine struct {
	VariantID string
	DesignID  string
}

// CartTarget is the storefront cart page. It never carries a cart id.
type CartTarget struct {
	Path string
}

// CartCode classifies a session-cart failure for HTTP mapping.
type CartCode string

const (
	CartNotFound      CartCode = "not_found"
	CartUnprocessable CartCode = "unprocessable"
	CartConflict      CartCode = "conflict"
	CartUnavailable   CartCode = "unavailable"
)

// CartError is a classified failure from SessionCart.
type CartError struct {
	Code CartCode
	Err  error
}

func (e *CartError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return string(e.Code) + ": " + e.Err.Error()
	}
	return string(e.Code)
}

func (e *CartError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SessionCart adds a design line to the caller's storefront session cart.
// The composition root supplies it. Implementations must not take a field code
// or a trusted-write marker from the caller beyond the context already granted.
type SessionCart interface {
	BelongsToCustomTee(ctx context.Context, variantID string) (bool, error)
	AddLine(ctx context.Context, w http.ResponseWriter, r *http.Request, line DesignLine) (CartTarget, error)
}
