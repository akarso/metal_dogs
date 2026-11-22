package metaldogs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const (
	designUnavailableMessage   = "Design no longer available"
	checkoutUnavailableMessage = "Checkout validation unavailable"
	designUnavailableCode      = "plugin.metaldogs.design_unavailable"
	validationUnavailableCode  = "plugin.metaldogs.validation_unavailable"
	designCheckoutStepName     = "metaldogs_design_sale"
	checkoutDesignRefusedEvent = "metaldogs.checkout.design_refused"
	checkoutUnavailableEvent   = "metaldogs.checkout.unavailable"
)

var errDesignStoreUnavailable = errors.New("metaldogs: design store is not wired")

func registerDesignCheckout(h pluginsdk.Host, p *Plugin) {
	h.RegisterCheckoutStep(
		&designCheckoutStep{plugin: p},
		string(pluginsdk.CheckoutBefore(extapi.CheckoutStepReserveInventory)),
	)
}

type designCheckoutStep struct {
	plugin *Plugin
}

func (s *designCheckoutStep) Name() string { return designCheckoutStepName }

func (s *designCheckoutStep) Execute(ctx context.Context, cctx extapi.CheckoutContext) error {
	cartID := ""
	customerID := ""
	if cctx != nil {
		cartID = cctx.CartID()
		customerID = cctx.CustomerID()
	}
	if cctx == nil || strings.TrimSpace(cartID) == "" {
		return checkoutUnavailable(s.plugin, cartID, errors.New("metaldogs: checkout cart id is missing"))
	}
	p := s.plugin
	if p == nil || p.host == nil {
		return checkoutUnavailable(p, cartID, errors.New("metaldogs: checkout plugin is not wired"))
	}
	port, ok := p.host.Cart()
	if !ok || port == nil {
		return checkoutUnavailable(p, cartID, errors.New("metaldogs: cart port is not wired"))
	}
	catalog, ok := p.host.Catalog()
	if !ok || catalog == nil {
		return checkoutUnavailable(p, cartID, errors.New("metaldogs: cart catalog is not wired"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, cartValidateTimeout)
	defer cancel()

	cart, err := port.Get(ctx, cartID, customerID)
	if err != nil || cart == nil {
		if err == nil {
			err = errors.New("metaldogs: checkout cart is missing")
		}
		return checkoutUnavailable(p, cartID, err)
	}
	if len(cart.Items) == 0 {
		return nil
	}
	variantIDs := make([]string, len(cart.Items))
	for i, item := range cart.Items {
		variantIDs[i] = item.VariantID
	}
	found, err := catalog.FindVariants(ctx, variantIDs)
	if err != nil {
		return checkoutUnavailable(p, cartID, err)
	}
	tees, unknown := splitTeeLines(cart.Items, found)
	if len(tees) == 0 && len(unknown) == 0 {
		return nil
	}
	now := time.Now().UTC()
	if p.designs != nil && p.designs.Clock != nil {
		now = p.designs.Clock.Now().UTC()
	}
	var lines []interface{}
	for _, item := range cart.Items {
		if _, ok := unknown[item.VariantID]; ok {
			lines = append(lines, refusalLine(item.VariantID, clientIssueCode(ports.ProductUnavailableCode)))
			continue
		}
		if _, ok := tees[item.VariantID]; !ok {
			continue
		}
		designID, present := lineDesignID(item)
		var foundDesign domain.Design
		known := false
		if present {
			if p.designs == nil || p.designs.Designs == nil {
				return checkoutUnavailable(p, cartID, errDesignStoreUnavailable)
			}
			foundDesign, known, err = p.designs.Designs.GetByID(ctx, designID)
			if err != nil {
				return checkoutUnavailable(p, cartID, err)
			}
		}
		if issue, blocked := validateDesignLine(designLineInput{
			Now: now, DesignID: designID, Present: present, Design: foundDesign, Found: known,
		}); blocked {
			lines = append(lines, refusalLine(item.VariantID, clientIssueCode(issue.Code)))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	logDesignRefusal(p, cartID, lines)
	return pluginsdk.ValidationError(designUnavailableMessage,
		pluginsdk.WithClientCode(designUnavailableCode),
		pluginsdk.WithDetails(map[string]interface{}{
			"lines":     lines,
			"retryable": false,
		}),
	)
}

func refusalLine(variantID, code string) map[string]interface{} {
	return map[string]interface{}{
		"variant_id": variantID,
		"code":       code,
	}
}

func clientIssueCode(code string) string {
	return strings.TrimPrefix(code, "metaldogs.")
}

func logDesignRefusal(p *Plugin, cartID string, lines []interface{}) {
	log := checkoutLogger(p)
	if log == nil {
		return
	}
	variantIDs := make([]string, 0, len(lines))
	codes := make([]string, 0, len(lines))
	for _, raw := range lines {
		line, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		variantID, _ := line["variant_id"].(string)
		code, _ := line["code"].(string)
		variantIDs = append(variantIDs, variantID)
		codes = append(codes, code)
	}
	log.Warn(checkoutDesignRefusedEvent, map[string]interface{}{
		"cart_id":     cartID,
		"variant_ids": variantIDs,
		"codes":       codes,
	})
}

func checkoutUnavailable(p *Plugin, cartID string, err error) error {
	if err == nil {
		err = errors.New("metaldogs: checkout validation failed")
	}
	if log := checkoutLogger(p); log != nil {
		log.Error(checkoutUnavailableEvent, err, map[string]interface{}{
			"cart_id": cartID,
		})
	}
	return pluginsdk.UnavailableError(checkoutUnavailableMessage,
		pluginsdk.WithClientCode(validationUnavailableCode),
		pluginsdk.WithDetails(map[string]interface{}{
			"retryable": true,
		}),
	)
}

func checkoutLogger(p *Plugin) pluginsdk.Logger {
	if p == nil || p.host == nil {
		return nil
	}
	return p.host.Logger()
}
