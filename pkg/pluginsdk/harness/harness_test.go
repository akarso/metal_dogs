package harness_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/pluginsdk/harness"
	"github.com/akarso/shopanda/plugins/extref"
)

type recordingCart struct {
	called bool
}

func (r *recordingCart) AddItem(context.Context, string, string, string, int, pluginsdk.CartAddItemOpts) error {
	r.called = true
	return nil
}

func (r *recordingCart) Get(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}

func (r *recordingCart) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}

func (r *recordingCart) EnsureStorefrontCart(context.Context, http.ResponseWriter, *http.Request, string) (*extapi.CartView, error) {
	return nil, nil
}

func (r *recordingCart) DeleteLineExtension(context.Context, string, string, string, string) error {
	return nil
}

func (r *recordingCart) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	return nil
}

func TestAdaptInit_ExtRef(t *testing.T) {
	res, err := harness.AdaptInit(extref.New(), harness.Options{
		Satisfy: []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	if res.Host == nil {
		t.Fatal("expected Host")
	}
}

func TestAdaptInit_CartPortBound(t *testing.T) {
	rec := &recordingCart{}
	res, err := harness.AdaptInit(extref.New(), harness.Options{
		CartPort: rec,
		Satisfy:  []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	err = extref.AddItemWithGiftNote(context.Background(), res.Host, "c1", "cust", "v1", 1, "hi")
	if err == nil || !strings.Contains(err.Error(), "cart port not bound") {
		t.Fatalf("non-owner CartPort must fail closed, got %v", err)
	}
	if rec.called {
		t.Fatal("AddItem must not run on a CartPort that cannot scope the owner")
	}
}
