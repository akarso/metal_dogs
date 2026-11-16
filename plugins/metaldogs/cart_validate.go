package metaldogs

import (
	"context"
	"fmt"
	"strings"
	"time"

	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func registerCartValidation(app *plugin.App, p *Plugin) error {
	if app == nil || p == nil {
		return fmt.Errorf("metaldogs plugin: cart validation: app not configured")
	}
	return app.Hooks(p.Name()).Register(extapi.HookCartValidate, 100, p.onCartValidate)
}

func (p *Plugin) onCartValidate(hctx *extapi.HookContext) error {
	if p == nil || p.sessionCart == nil || p.designs == nil || p.designs.Designs == nil {
		return nil
	}
	raw, ok := hctx.Get("cart")
	if !ok || raw == nil {
		return nil
	}
	current, ok := raw.(*domaincart.Cart)
	if !ok || current == nil {
		return nil
	}
	// Hook handlers are not given the request context.
	ctx := context.Background()
	now := time.Now().UTC()
	if p.designs.Clock != nil {
		now = p.designs.Clock.Now().UTC()
	}
	for _, item := range current.Items {
		tee, err := p.sessionCart.BelongsToCustomTee(ctx, item.VariantID)
		if err != nil {
			return err
		}
		if !tee {
			continue
		}
		designID, present, err := p.sessionCart.LineDesign(ctx, current.ID, item.VariantID)
		if err != nil {
			return err
		}
		var found domain.Design
		ok := false
		if present {
			found, ok, err = p.designs.Designs.GetByID(ctx, designID)
			if err != nil {
				return err
			}
		}
		issue, blocked := designCartIssue(now, item.VariantID, designID, present, found, ok)
		if blocked {
			hctx.AppendValidationError(issue)
		}
	}
	return nil
}

func designCartIssue(now time.Time, variantID, designID string, present bool, d domain.Design, found bool) (extapi.CartValidationIssue, bool) {
	issue := extapi.CartValidationIssue{VariantID: variantID}
	if !present || strings.TrimSpace(designID) == "" {
		issue.Code = ports.DesignRequiredCode
		issue.Message = "design required"
		return issue, true
	}
	if !found {
		issue.Code = ports.DesignMismatchCode
		issue.Message = "design does not match the custom tee"
		return issue, true
	}
	if d.Status == domain.DesignExpired || !d.ExpiresAt.After(now) {
		issue.Code = ports.DesignExpiredCode
		issue.Message = "design has expired"
		return issue, true
	}
	if d.Status != domain.DesignApproved {
		issue.Code = ports.DesignNotApprovedCode
		issue.Message = "design is not approved"
		return issue, true
	}
	return extapi.CartValidationIssue{}, false
}
