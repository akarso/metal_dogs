package httpapi

import "net/http"

// NotFound is GET /api/customizer. Child NoCache prefixes do not cover this path.
func NotFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSONError(w, http.StatusNotFound, "not_found")
	})
}
