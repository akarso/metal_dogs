package main

import (
	"sync"

	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/sessioncart"
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
	port := sessioncart.New(rt.cartService, rt.repos.productRepo, rt.repos.variantRepo)
	metalDogsMu.Lock()
	p := metalDogsPlugin
	metalDogsMu.Unlock()
	if p != nil {
		p.UseSessionCart(port)
	}
}
