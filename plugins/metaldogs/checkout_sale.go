package metaldogs

import (
	"context"
	"errors"
	"net/http"
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

// FailCartGetForTest makes the checkout step's cart read return err.
// BindCartPort cannot: an attributed Host.Cart accepts only the SDK cart adapter, so any other CartPort is reported unwired and Get is never called.
func (p *Plugin) FailCartGetForTest(err error) {
	if p == nil {
		return
	}
	if err == nil {
		err = errors.New("metaldogs: cart get failed")
	}
	p.host = cartGetErrorSurface{cartSurface: p.host, err: err}
}

// FailCatalogFindForTest makes the checkout step's FindVariants return err.
func (p *Plugin) FailCatalogFindForTest(err error) {
	if p == nil {
		return
	}
	if err == nil {
		err = errors.New("metaldogs: catalog find failed")
	}
	p.host = catalogFindErrorSurface{cartSurface: p.host, err: err}
}

type cartGetErrorSurface struct {
	cartSurface
	err error
}

func (s cartGetErrorSurface) Cart() (pluginsdk.CartPort, bool) {
	return errCartPort{err: s.err}, true
}

type errCartPort struct{ err error }

func (p errCartPort) Get(context.Context, string, string) (*extapi.CartView, error) {
	return nil, p.err
}
func (errCartPort) AddItem(context.Context, string, string, string, int, pluginsdk.CartAddItemOpts) error {
	return nil
}
func (errCartPort) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}
func (errCartPort) EnsureStorefrontCart(context.Context, http.ResponseWriter, *http.Request, string) (*extapi.CartView, error) {
	return nil, nil
}
func (errCartPort) DiscardEmptyGuestCart(context.Context, http.ResponseWriter, *http.Request, string) error {
	return nil
}
func (errCartPort) DeleteLineExtension(context.Context, string, string, string, string) error {
	return nil
}
func (errCartPort) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	return nil
}

type catalogFindErrorSurface struct {
	cartSurface
	err error
}

func (s catalogFindErrorSurface) Catalog() (pluginsdk.CatalogPort, bool) {
	inner, ok := s.cartSurface.Catalog()
	if !ok || inner == nil {
		return nil, false
	}
	return catalogFindError{CatalogPort: inner, err: s.err}, true
}

type catalogFindError struct {
	pluginsdk.CatalogPort
	err error
}

func (c catalogFindError) FindVariants(context.Context, []string) (map[string]*extapi.CatalogVariant, error) {
	return nil, c.err
}
