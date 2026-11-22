package metaldogs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

const designUnavailableMessage = "Design no longer available"

const designCheckoutStepName = "metaldogs_design_sale"

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
	if cctx == nil || strings.TrimSpace(cctx.CartID()) == "" {
		return checkoutUnavailable(errors.New("metaldogs: checkout cart id is missing"))
	}
	p := s.plugin
	if p == nil || p.host == nil {
		return checkoutUnavailable(errors.New("metaldogs: checkout plugin is not wired"))
	}
	port, ok := p.host.Cart()
	if !ok || port == nil {
		return checkoutUnavailable(errors.New("metaldogs: cart port is not wired"))
	}
	catalog, ok := p.host.Catalog()
	if !ok || catalog == nil {
		return checkoutUnavailable(errors.New("metaldogs: cart catalog is not wired"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, cartValidateTimeout)
	defer cancel()

	cart, err := port.Get(ctx, cctx.CartID(), cctx.CustomerID())
	if err != nil || cart == nil {
		return checkoutUnavailable(err)
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
		return checkoutUnavailable(err)
	}
	tees, unknown := splitTeeLines(cart.Items, found)
	if len(tees) == 0 && len(unknown) == 0 {
		return nil
	}
	now := time.Now().UTC()
	if p.designs != nil && p.designs.Clock != nil {
		now = p.designs.Clock.Now().UTC()
	}
	invalid := false
	for _, item := range cart.Items {
		if _, ok := unknown[item.VariantID]; ok {
			invalid = true
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
				return checkoutUnavailable(errDesignStoreUnavailable)
			}
			foundDesign, known, err = p.designs.Designs.GetByID(ctx, designID)
			if err != nil {
				return checkoutUnavailable(err)
			}
		}
		if _, blocked := ValidateDesignLine(DesignLineInput{
			Now: now, DesignID: designID, Present: present, Design: foundDesign, Found: known,
		}); blocked {
			invalid = true
		}
	}
	if !invalid {
		return nil
	}
	return pluginsdk.ValidationError(designUnavailableMessage)
}

func checkoutUnavailable(err error) error {
	return pluginsdk.InternalError("metaldogs: checkout validation unavailable", err)
}
