package main

import (
	"sync"

	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/sessioncart"
)

var (
	metalDogsMu      sync.Mutex
	metalDogsPlugins []*metaldogs.Plugin
)

func rememberMetalDogs(p *metaldogs.Plugin) {
	if p == nil {
		return
	}
	metalDogsMu.Lock()
	metalDogsPlugins = append(metalDogsPlugins, p)
	metalDogsMu.Unlock()
}

// bindMetalDogsSessionCart supplies the session-cart port after the cart service exists.
func bindMetalDogsSessionCart(rt *serveRuntime) {
	if rt == nil || rt.cartService == nil || rt.repos == nil || rt.repos.productRepo == nil || rt.repos.variantRepo == nil {
		return
	}
	port := sessioncart.New(rt.cartService, rt.repos.productRepo, rt.repos.variantRepo)
	metalDogsMu.Lock()
	plugins := append([]*metaldogs.Plugin(nil), metalDogsPlugins...)
	metalDogsMu.Unlock()
	for _, p := range plugins {
		p.UseSessionCart(port)
	}
}
