package application

import (
	"context"
	"errors"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// AddToCart puts an approved design on the caller's session cart.
type AddToCart struct {
	Designs *DesignService
	Cart    ports.SessionCart
	// Grant marks ctx as a trusted write for the fixed design field. The route
	// calls it immediately before the cart write; it must not read the request.
	Grant func(context.Context) (context.Context, error)
}

// Execute authorises the design and adds one line.
// The returned session is the cookie value; path is the cart page.
func (a *AddToCart) Execute(ctx context.Context, session ports.CartSession, designID, token, variantID string) (ports.CartSession, string, error) {
	if a == nil || a.Designs == nil || a.Cart == nil || a.Grant == nil {
		return "", "", &DesignError{Code: DesignErrUnavailable, Message: "cart unavailable"}
	}
	d, _, err := a.Designs.loadAuthorized(ctx, designID, token)
	if err != nil {
		return "", "", err
	}
	if d.Status != domain.DesignApproved {
		return "", "", &DesignError{Code: DesignErrInvalidTransition, Message: "design is not ready"}
	}
	onTee, err := a.Cart.BelongsToCustomTee(ctx, variantID)
	if err != nil {
		return "", "", mapCartErr(err)
	}
	if !onTee {
		return "", "", &DesignError{Code: DesignErrUnprocessable, Message: "variant is not part of the custom tee"}
	}
	writeCtx, err := a.Grant(ctx)
	if err != nil {
		return "", "", &DesignError{Code: DesignErrUnavailable, Message: "cart unavailable", Cause: err}
	}
	next, target, err := a.Cart.AddLine(writeCtx, session, ports.DesignLine{VariantID: variantID, DesignID: d.ID})
	if err != nil {
		return "", "", mapCartErr(err)
	}
	if next == "" || target.Path == "" {
		return "", "", &DesignError{Code: DesignErrUnavailable, Message: "cart unavailable"}
	}
	return next, target.Path, nil
}

func mapCartErr(err error) error {
	if err == nil {
		return nil
	}
	var de *DesignError
	if errors.As(err, &de) {
		return err
	}
	var ce *ports.CartError
	if !errors.As(err, &ce) {
		return &DesignError{Code: DesignErrUnavailable, Message: "cart unavailable", Cause: err}
	}
	switch ce.Code {
	case ports.CartNotFound:
		return &DesignError{Code: DesignErrNotFound, Message: "not found"}
	case ports.CartUnprocessable:
		return &DesignError{Code: DesignErrUnprocessable, Message: "unprocessable"}
	case ports.CartConflict:
		return &DesignError{Code: DesignErrConflict, Message: "conflict"}
	default:
		return &DesignError{Code: DesignErrUnavailable, Message: "cart unavailable", Cause: ce.Err}
	}
}
