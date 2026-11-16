package main

import (
	"context"
	"fmt"
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
// An enabled plugin fails closed when the port cannot be bound; a disabled plugin only logs.
func bindMetalDogsSessionCart(rt *serveRuntime) error {
	if rt == nil || rt.cartService == nil || rt.repos == nil || rt.repos.productRepo == nil || rt.repos.variantRepo == nil {
		if rt != nil && rt.log != nil {
			rt.log.Warn("metaldogs: session cart bind skipped", map[string]interface{}{
				"reason": "cart service or catalog repositories are not configured",
			})
		}
		metalDogsMu.Lock()
		enabled := metalDogsPlugin != nil
		metalDogsMu.Unlock()
		if enabled {
			return fmt.Errorf("metaldogs: session cart port is required while the plugin is enabled")
		}
		return nil
	}
	var grant func(context.Context) (context.Context, error)
	if rt.pluginApp != nil {
		grant = func(ctx context.Context) (context.Context, error) {
			return rt.pluginApp.PluginWriteContext(ctx, metaldogs.PluginName, ports.DesignIDFieldCode)
		}
	}
	port := sessioncart.New(rt.cartService, rt.repos.productRepo, rt.repos.variantRepo, rt.extensionValueService, rt.repos.cartRepo, grant)
	metalDogsMu.Lock()
	p := metalDogsPlugin
	metalDogsMu.Unlock()
	if p != nil {
		p.UseSessionCart(port)
	}
	return nil
}
