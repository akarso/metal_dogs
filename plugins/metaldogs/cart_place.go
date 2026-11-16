package metaldogs

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strconv"
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
	token := p.bindPending(cart.ID, variantID)
	defer p.releasePending(token)
	writeCtx := p.host.WithPluginWrite(ctx, PluginName, ports.DesignIDFieldCode)
	if err := port.AddItem(writeCtx, cart.ID, cart.CustomerID, variantID, 1, pluginsdk.CartAddItemOpts{
		Extensions: []pluginsdk.CartExtensionValue{{
			Code:  ports.DesignIDFieldCode,
			Value: designID,
		}},
		UpdatedBy: cartLineActor(cart.CustomerID),
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
	p.rememberPending(hookString(hctx, "cart_id"), hookString(hctx, "variant_id"), designID)
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

type pendingCall struct {
	token     uint64
	cartID    string
	variantID string
	designID  string
}

func cartLineActor(customerID string) string {
	if id := strings.TrimSpace(customerID); id != "" {
		return id
	}
	return "guest"
}

// Hook handlers do not receive Place's context; before-hook and cart.validate share the AddItem goroutine.
func currentGID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	fields := strings.Fields(string(buf[:n]))
	if len(fields) < 2 {
		return 0
	}
	id, _ := strconv.ParseUint(fields[1], 10, 64)
	return id
}

func (p *Plugin) bindPending(cartID, variantID string) uint64 {
	if p == nil || cartID == "" || variantID == "" {
		return 0
	}
	gid := currentGID()
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.pending == nil {
		p.pending = map[uint64]pendingCall{}
	}
	p.pendingSeq++
	token := p.pendingSeq
	p.pending[gid] = pendingCall{token: token, cartID: cartID, variantID: variantID}
	return token
}

func (p *Plugin) rememberPending(cartID, variantID, designID string) {
	if p == nil || cartID == "" || variantID == "" || designID == "" {
		return
	}
	gid := currentGID()
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.pending == nil {
		p.pending = map[uint64]pendingCall{}
	}
	if cur, ok := p.pending[gid]; ok && cur.cartID == cartID && cur.variantID == variantID {
		cur.designID = designID
		p.pending[gid] = cur
		return
	}
	p.pendingSeq++
	p.pending[gid] = pendingCall{
		token: p.pendingSeq, cartID: cartID, variantID: variantID, designID: designID,
	}
}

func (p *Plugin) setPendingDesign(cartID, variantID, designID string) {
	p.rememberPending(cartID, variantID, designID)
}

func (p *Plugin) pendingDesign(cartID, variantID string) (string, bool) {
	if p == nil {
		return "", false
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	cur, ok := p.pending[currentGID()]
	if !ok || cur.cartID != cartID || cur.variantID != variantID {
		return "", false
	}
	id := strings.TrimSpace(cur.designID)
	return id, id != ""
}

func (p *Plugin) releasePending(token uint64) {
	if p == nil || token == 0 {
		return
	}
	gid := currentGID()
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	cur, ok := p.pending[gid]
	if !ok || cur.token != token {
		return
	}
	delete(p.pending, gid)
}
