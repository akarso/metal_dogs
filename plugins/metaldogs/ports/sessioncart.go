package ports

import "context"

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

// CartSession is the storefront cart id from the session cookie.
// Empty means the caller has no cart yet. It must not be written into a response body.
type CartSession string

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
	// AddLine adds a quantity-one line, creating a guest cart when the session is empty.
	// The same design on that variant is left unchanged; any other occupant is a conflict.
	AddLine(ctx context.Context, session CartSession, line DesignLine) (CartSession, CartTarget, error)
}
