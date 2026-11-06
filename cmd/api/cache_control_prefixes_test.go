package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
)

func TestCacheControl_CustomizerEnabled_NoStore(t *testing.T) {
	t.Parallel()
	mw := shophttp.CacheControlMiddleware(cacheControlNoCachePrefixes())
	h := mw(httpapi.Enabled())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/enabled", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store (customizer must be on the no-cache prefix list)", got)
	}
}

func TestCacheControl_CustomizerPrefix_CoversSubtree(t *testing.T) {
	t.Parallel()
	mw := shophttp.CacheControlMiddleware(cacheControlNoCachePrefixes())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/api/customizer", "/api/customizer/enabled", "/api/customizer/templates"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q, want no-store", path, got)
		}
	}
}
