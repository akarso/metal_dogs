package application

import (
	"context"
	"net/http"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

// DesignLineCart puts one approved design on the storefront cart.
// cartURL is the cart page. The cart id stays out of the response body.
type DesignLineCart interface {
	Place(ctx context.Context, w http.ResponseWriter, r *http.Request, variantID, designID string) (cartURL string, err error)
}

// AddToCart puts an approved design on the caller's storefront cart.
type AddToCart struct {
	Designs *DesignService
	Cart    DesignLineCart
}

// Execute authorises the design and adds one line.
func (a *AddToCart) Execute(ctx context.Context, w http.ResponseWriter, r *http.Request, designID, token, variantID string) (string, error) {
	if a == nil || a.Designs == nil || a.Cart == nil {
		return "", &DesignError{Code: DesignErrUnavailable, Message: "cart unavailable"}
	}
	d, _, err := a.Designs.loadAuthorized(ctx, designID, token)
	if err != nil {
		return "", err
	}
	// Re-order uses CloneDraft. This route stays approved-only (409 for ordered).
	if d.Status != domain.DesignApproved {
		return "", &DesignError{Code: DesignErrInvalidTransition, Message: "design is not ready"}
	}
	return a.Cart.Place(ctx, w, r, variantID, d.ID)
}
