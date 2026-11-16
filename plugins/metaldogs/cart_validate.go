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
	if len(current.Items) == 0 {
		return nil
	}
	variantIDs := make([]string, len(current.Items))
	for i, item := range current.Items {
		variantIDs[i] = item.VariantID
	}
	tees, err := p.sessionCart.CustomTeeLines(ctx, variantIDs)
	if err != nil {
		return err
	}
	if len(tees) == 0 {
		return nil
	}
	// RemoveItem deletes extension values before this hook and does not restore them when validation fails.
	// Issues on lines this mutation left unchanged stay on the cart read.
	persisted, stored, err := p.sessionCart.PersistedLineQuantities(ctx, current.ID)
	if err != nil {
		return err
	}
	scope := teeLinesToValidate(current.Items, persisted, stored, tees)
	for _, item := range current.Items {
		if _, ok := scope[item.VariantID]; !ok {
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
		issue.Code = ports.DesignNotFoundCode
		issue.Message = "design not found"
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

func teeLinesToValidate(proposed []domaincart.Item, persisted map[string]int, persistedFound bool, tees map[string]struct{}) map[string]struct{} {
	if len(tees) == 0 {
		return nil
	}
	proposedQty := make(map[string]int, len(proposed))
	for _, item := range proposed {
		proposedQty[item.VariantID] = item.Quantity
	}
	changed := !persistedFound
	if persistedFound {
		if len(proposedQty) != len(persisted) {
			changed = true
		} else {
			for id, qty := range proposedQty {
				if persisted[id] != qty {
					changed = true
					break
				}
			}
		}
	}
	out := make(map[string]struct{})
	for id := range tees {
		if _, onLine := proposedQty[id]; !onLine {
			continue
		}
		if changed && persistedFound && persisted[id] == proposedQty[id] {
			continue
		}
		out[id] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
