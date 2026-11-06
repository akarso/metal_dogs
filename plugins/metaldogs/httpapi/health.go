package httpapi

import "net/http"

// Health returns GET /api/customizer/health.
func Health() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","plugin":"metaldogs"}`))
	})
}
