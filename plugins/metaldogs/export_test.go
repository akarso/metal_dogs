package metaldogs

import (
	"context"
	"errors"
	"net/http"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func (p *Plugin) OrderSource() ports.OrderSource {
	if p == nil {
		return nil
	}
	return p.orderSource
}

func (p *Plugin) SetSnapshotClock(c ports.Clock) {
	if p != nil {
		p.snapshotClock = c
	}
}

// Host.Cart returns a bound port only for the SDK *cartPort, so a failing Get wraps the host instead of BindCartPort.
func (p *Plugin) FailCartGetForTest(err error) {
	if p == nil {
		return
	}
	if err == nil {
		err = errors.New("metaldogs: cart get failed")
	}
	p.host = cartGetErrorSurface{cartSurface: p.host, err: err}
}

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
