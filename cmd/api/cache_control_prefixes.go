package main

// cacheControlNoCachePrefixes lists path prefixes that must never receive
// Cache-Control: public. Guest GETs outside this list default to max-age=300.
// Keep /api/customizer here: enablement and later customizer GETs must not be
// cached across flag flips or design ownership changes.
func cacheControlNoCachePrefixes() []string {
	return []string{
		"/setup",
		"/fragments",
		"/storefront",
		"/api/v1/setup",
		"/api/v1/carts",
		"/api/v1/checkout",
		"/api/v1/orders",
		"/api/v1/account",
		"/api/v1/auth",
		"/api/v1/shipping",
		"/api/v1/admin",
		"/api/customizer",
	}
}
