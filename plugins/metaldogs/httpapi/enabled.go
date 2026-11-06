package httpapi

import "net/http"

// Enabled returns GET /api/customizer/enabled.
//
// This is an enablement probe for the Metal Dogs plugin flag — not a process
// liveness or readiness check. Orchestrators must continue to use /healthz and
// /readyz (mounted outside StoreMiddleware). This handler shares the full API
// middleware stack and can hang if Postgres is unavailable.
func Enabled() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","plugin":"metaldogs"}`))
	})
}
