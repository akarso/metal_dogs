package main

import (
	"context"
	"sync"

	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/sessioncart"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

var (
	metalDogsMu     sync.Mutex
	metalDogsPlugin *metaldogs.Plugin
)

func rememberMetalDogs(p *metaldogs.Plugin) {
	if p == nil {
		return
	}
	metalDogsMu.Lock()
	metalDogsPlugin = p
	metalDogsMu.Unlock()
}

// bindMetalDogsSessionCart supplies the session-cart port after the cart service exists.
func bindMetalDogsSessionCart(rt *serveRuntime) {
	if rt == nil || rt.cartService == nil || rt.repos == nil || rt.repos.productRepo == nil || rt.repos.variantRepo == nil {
		return
	}
	var grant func(context.Context) (context.Context, error)
	if rt.pluginApp != nil {
		grant = func(ctx context.Context) (context.Context, error) {
			return rt.pluginApp.PluginWriteContext(ctx, metaldogs.PluginName, ports.DesignIDFieldCode)
		}
	}
	port := sessioncart.New(rt.cartService, rt.repos.productRepo, rt.repos.variantRepo, rt.extensionValueService, grant)
	metalDogsMu.Lock()
	p := metalDogsPlugin
	metalDogsMu.Unlock()
	if p != nil {
		p.UseSessionCart(port)
	}
}
