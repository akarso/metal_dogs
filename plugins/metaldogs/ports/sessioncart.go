package ports

import "context"

// DesignIDFieldCode is the cart-line extension written only by the add-to-cart route.
const DesignIDFieldCode = "metaldogs.design_id"

// Cart-validation issue codes. Level is left empty so each issue blocks checkout.
const (
	DesignRequiredCode    = "metaldogs.design_required"
	DesignNotApprovedCode = "metaldogs.design_not_approved"
	DesignExpiredCode     = "metaldogs.design_expired"
	DesignMismatchCode    = "metaldogs.design_mismatch"
)

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
	// LineDesign reads the design id stored for a cart line.
	// present is false when the line has no design id, including before the line itself is saved.
	LineDesign(ctx context.Context, cartID, variantID string) (designID string, present bool, err error)
	// AddLine adds a quantity-one line, creating a guest cart when the session is empty.
	// The same design on that variant is left unchanged; any other occupant is a conflict.
	AddLine(ctx context.Context, session CartSession, line DesignLine) (CartSession, CartTarget, error)
}
