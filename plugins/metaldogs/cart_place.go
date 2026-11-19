package metaldogs

import (
	"context"
	"errors"
	"fmt"
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
	class, err := variantTeeClass(ctx, catalog, variantID)
	if err != nil {
		return "", cartPlaceErr(err)
	}
	switch class {
	case extapi.ProductSlugMatch:
	case extapi.ProductSlugOther:
		return "", &application.DesignError{Code: application.DesignErrUnprocessable, Message: "variant is not part of the custom tee"}
	default:
		return "", &application.DesignError{Code: application.DesignErrUnprocessable, Message: "product is not available"}
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
	writeCtx := p.host.WithPluginWrite(ctx, PluginName, ports.DesignIDFieldCode)
	if err := port.AddItem(writeCtx, cart.ID, cart.CustomerID, variantID, 1, pluginsdk.CartAddItemOpts{
		Extensions: []pluginsdk.CartExtensionValue{{
			Code:  ports.DesignIDFieldCode,
			Value: designID,
		}},
		UpdatedBy: cartLineActor(cart.CustomerID),
	}); err != nil {
		if len(cart.Items) == 0 {
			discardCtx, cancel := context.WithTimeout(context.Background(), cartValidateTimeout)
			defer cancel()
			if derr := port.DiscardEmptyGuestCart(discardCtx, w, r, cart.ID); derr != nil {
				return "", cartPlaceErr(fmt.Errorf("%w (also discard empty guest cart: %v)", err, derr))
			}
		}
		return "", cartPlaceErr(err)
	}
	finishGuestCookie(w, r, cart)
	return cartPagePath, nil
}

func variantTeeClass(ctx context.Context, catalog pluginsdk.CatalogPort, variantID string) (extapi.ProductSlugClass, error) {
	found, err := catalog.FindVariants(ctx, []string{variantID})
	if err != nil {
		return extapi.ProductSlugUnknown, err
	}
	return extapi.ClassifyProductSlug(found[strings.TrimSpace(variantID)], customTeeSlug), nil
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

func cartLineActor(customerID string) string {
	if id := strings.TrimSpace(customerID); id != "" {
		return id
	}
	return "guest"
}
