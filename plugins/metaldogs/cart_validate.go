package metaldogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const (
	cartValidateTimeout = 5 * time.Second
	customTeeSlug       = "mdogs-tee"
)

var errUnexpectedCartPayload = errors.New("metaldogs: cart.validate payload is not a cart")

func registerCartValidation(app *plugin.App, p *Plugin) error {
	if app == nil || p == nil {
		return fmt.Errorf("metaldogs plugin: cart validation: app not configured")
	}
	return app.Hooks(p.Name()).Register(extapi.HookCartValidate, 100, p.onCartValidate)
}

func (p *Plugin) onCartValidate(hctx *extapi.HookContext) error {
	if hctx == nil {
		return nil
	}
	raw, ok := hctx.Get("cart")
	if !ok || raw == nil {
		return nil
	}
	current, err := cartFromValidatePayload(raw)
	if err != nil {
		if p != nil && p.host != nil && p.host.Logger() != nil {
			p.host.Logger().Error("metaldogs.cart.validate.payload", err, map[string]interface{}{
				"hook": "cart.validate",
			})
		}
		return err
	}
	if current == nil || len(current.Items) == 0 {
		return nil
	}
	if p == nil || p.host == nil {
		return fmt.Errorf("metaldogs: cart catalog is not wired")
	}
	catalog, ok := p.host.Catalog()
	if !ok || catalog == nil {
		return fmt.Errorf("metaldogs: cart catalog is not wired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cartValidateTimeout)
	defer cancel()
	variantIDs := make([]string, len(current.Items))
	for i, item := range current.Items {
		variantIDs[i] = item.VariantID
	}
	found, err := catalog.FindVariants(ctx, variantIDs)
	if err != nil {
		return err
	}
	tees, unknown := splitTeeLines(current.Items, found)
	for _, item := range current.Items {
		if _, ok := unknown[item.VariantID]; !ok {
			continue
		}
		hctx.AppendValidationError(productUnavailableIssue(item.VariantID))
	}
	if len(tees) == 0 {
		return nil
	}
	port, ok := p.host.Cart()
	if !ok || port == nil {
		return fmt.Errorf("metaldogs: cart port is not wired")
	}
	persisted, stored, err := persistedLineQuantities(ctx, port, current.ID, current.CustomerID)
	if err != nil {
		return err
	}
	scope := teeLinesToValidate(current.Items, persisted, stored, tees)
	now := time.Now().UTC()
	if p.designs != nil && p.designs.Clock != nil {
		now = p.designs.Clock.Now().UTC()
	}
	for _, item := range current.Items {
		if _, ok := scope[item.VariantID]; !ok {
			continue
		}
		designID, present := lineDesignID(item)
		if id, ok := pendingLineDesign(hctx, item.VariantID); ok {
			designID, present = id, true
		}
		var foundDesign domain.Design
		known := false
		if present && p.designs != nil && p.designs.Designs != nil {
			foundDesign, known, err = p.designs.Designs.GetByID(ctx, designID)
			if err != nil {
				return err
			}
		}
		issue, blocked := designCartIssue(now, item.VariantID, designID, present, foundDesign, known)
		if blocked {
			hctx.AppendValidationError(issue)
		}
	}
	return nil
}

func cartFromValidatePayload(raw any) (*extapi.CartView, error) {
	if raw == nil {
		return nil, nil
	}
	current, ok := raw.(*extapi.CartView)
	if !ok {
		return nil, errUnexpectedCartPayload
	}
	return current, nil
}

func splitTeeLines(items []extapi.CartLineView, found map[string]*extapi.CatalogVariant) (match, unknown map[string]struct{}) {
	match = map[string]struct{}{}
	unknown = map[string]struct{}{}
	for _, item := range items {
		switch extapi.ClassifyProductSlug(found[item.VariantID], customTeeSlug) {
		case extapi.ProductSlugMatch:
			match[item.VariantID] = struct{}{}
		case extapi.ProductSlugOther:
		default:
			unknown[item.VariantID] = struct{}{}
		}
	}
	return match, unknown
}

func productUnavailableIssue(variantID string) extapi.CartValidationIssue {
	return extapi.CartValidationIssue{
		VariantID: variantID,
		Code:      ports.ProductUnavailableCode,
		Message:   "product is not available",
	}
}

func pendingLineDesign(hctx *extapi.HookContext, variantID string) (string, bool) {
	if hctx == nil {
		return "", false
	}
	raw, ok := hctx.Get("pending_line")
	if !ok || raw == nil {
		return "", false
	}
	pending, ok := raw.(*extapi.PendingCartLine)
	if !ok || pending == nil || pending.VariantID != variantID {
		return "", false
	}
	return lineDesignID(extapi.CartLineView{Extensions: pending.Extensions})
}

func persistedLineQuantities(ctx context.Context, port pluginsdk.CartPort, cartID, customerID string) (map[string]int, bool, error) {
	cartID = strings.TrimSpace(cartID)
	if cartID == "" {
		return map[string]int{}, false, nil
	}
	stored, err := port.Get(ctx, cartID, customerID)
	if err != nil {
		if pluginsdk.IsNotFound(err) {
			return map[string]int{}, false, nil
		}
		return nil, false, err
	}
	if stored == nil {
		return map[string]int{}, false, nil
	}
	qty := make(map[string]int, len(stored.Items))
	for _, item := range stored.Items {
		qty[item.VariantID] = item.Quantity
	}
	return qty, true, nil
}

func lineDesignID(line extapi.CartLineView) (string, bool) {
	for _, value := range line.Extensions {
		if value.Code != ports.DesignIDFieldCode {
			continue
		}
		id, _ := value.Value.(string)
		id = strings.TrimSpace(id)
		if id == "" {
			return "", false
		}
		return id, true
	}
	return "", false
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

func teeLinesToValidate(proposed []extapi.CartLineView, persisted map[string]int, persistedFound bool, tees map[string]struct{}) map[string]struct{} {
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
