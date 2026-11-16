package metaldogs

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/storefrontsession"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const cartPagePath = "/cart"

// Place adds one custom-tee line through Host.Cart and Host.Catalog.
// Reads use the request context. WithPluginWrite covers only AddItem.
func (p *Plugin) Place(ctx context.Context, w http.ResponseWriter, r *http.Request, variantID, designID string) (string, error) {
	if p == nil || p.host == nil {
		return "", cartUnavailable(nil)
	}
	catalog, ok := p.host.Catalog()
	if !ok || catalog == nil {
		return "", cartUnavailable(nil)
	}
	port, ok := p.host.Cart()
	if !ok || port == nil {
		return "", cartUnavailable(nil)
	}
	onTee, err := variantIsCustomTee(ctx, catalog, variantID)
	if err != nil {
		return "", cartPlaceErr(err)
	}
	if !onTee {
		return "", &application.DesignError{Code: application.DesignErrUnprocessable, Message: "variant is not part of the custom tee"}
	}
	cart, err := port.EnsureStorefrontCart(ctx, w, r, "")
	if err != nil {
		return "", cartPlaceErr(err)
	}
	if cart == nil || strings.TrimSpace(cart.ID) == "" {
		return "", cartUnavailable(nil)
	}
	defer p.clearPendingDesign(cart.ID, variantID)
	for _, line := range cart.Items {
		if line.VariantID != variantID {
			continue
		}
		existing, has := lineDesignID(line)
		if has && existing == designID {
			finishGuestCookie(w, r, cart)
			return cartPagePath, nil
		}
		return "", &application.DesignError{Code: application.DesignErrConflict, Message: "conflict"}
	}
	writeCtx := p.host.WithPluginWrite(ctx, PluginName, ports.DesignIDFieldCode)
	if err := port.AddItem(writeCtx, cart.ID, cart.CustomerID, variantID, 1, pluginsdk.CartAddItemOpts{
		Extensions: []pluginsdk.CartExtensionValue{{
			Code:  ports.DesignIDFieldCode,
			Value: designID,
		}},
		UpdatedBy: "guest",
	}); err != nil {
		return "", cartPlaceErr(err)
	}
	finishGuestCookie(w, r, cart)
	return cartPagePath, nil
}

func variantIsCustomTee(ctx context.Context, catalog pluginsdk.CatalogPort, variantID string) (bool, error) {
	found, err := catalog.FindVariants(ctx, []string{variantID})
	if err != nil {
		return false, err
	}
	variant := found[strings.TrimSpace(variantID)]
	return variant != nil && variant.ProductSlug == customTeeSlug, nil
}

func finishGuestCookie(w http.ResponseWriter, r *http.Request, cart *extapi.CartView) {
	if w == nil || cart == nil || strings.TrimSpace(cart.CustomerID) != "" || strings.TrimSpace(cart.ID) == "" {
		return
	}
	storefrontsession.SetCartCookie(w, r, cart.ID)
}

func cartUnavailable(err error) error {
	return &application.DesignError{Code: application.DesignErrUnavailable, Message: "cart unavailable", Cause: err}
}

func cartPlaceErr(err error) error {
	if err == nil {
		return nil
	}
	var de *application.DesignError
	if errors.As(err, &de) {
		return err
	}
	switch {
	case pluginsdk.IsNotFound(err):
		return &application.DesignError{Code: application.DesignErrNotFound, Message: "not found", Cause: err}
	case pluginsdk.IsConflict(err):
		return &application.DesignError{Code: application.DesignErrConflict, Message: "conflict", Cause: err}
	case pluginsdk.IsValidation(err):
		return &application.DesignError{Code: application.DesignErrUnprocessable, Message: "unprocessable", Cause: err}
	default:
		return cartUnavailable(err)
	}
}

func (p *Plugin) onAddItemBefore(hctx *extapi.HookContext) error {
	if hctx == nil || p == nil {
		return nil
	}
	raw, ok := hctx.Get("extensions")
	if !ok || raw == nil {
		return nil
	}
	designID := pendingDesignID(raw)
	if designID == "" {
		return nil
	}
	// cart.validate reads stored line extensions. AddItem upserts this field after that hook.
	p.setPendingDesign(hookString(hctx, "cart_id"), hookString(hctx, "variant_id"), designID)
	return nil
}

func pendingDesignID(raw any) string {
	rows, ok := raw.([]map[string]interface{})
	if !ok {
		return ""
	}
	for _, row := range rows {
		code, _ := row["field_code"].(string)
		if code != ports.DesignIDFieldCode {
			continue
		}
		value, _ := row["value"].(string)
		return strings.TrimSpace(value)
	}
	return ""
}

func hookString(hctx *extapi.HookContext, key string) string {
	raw, ok := hctx.Get(key)
	if !ok || raw == nil {
		return ""
	}
	s, _ := raw.(string)
	return strings.TrimSpace(s)
}

func pendingKey(cartID, variantID string) string {
	return cartID + "\x00" + variantID
}

func (p *Plugin) setPendingDesign(cartID, variantID, designID string) {
	if p == nil || cartID == "" || variantID == "" || designID == "" {
		return
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.pending == nil {
		p.pending = map[string]string{}
	}
	p.pending[pendingKey(cartID, variantID)] = designID
}

func (p *Plugin) pendingDesign(cartID, variantID string) (string, bool) {
	if p == nil {
		return "", false
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	id := strings.TrimSpace(p.pending[pendingKey(cartID, variantID)])
	return id, id != ""
}

func (p *Plugin) clearPendingDesign(cartID, variantID string) {
	if p == nil {
		return
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	delete(p.pending, pendingKey(cartID, variantID))
}
