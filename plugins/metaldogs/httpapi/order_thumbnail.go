package httpapi

import (
	"net/http"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

// orderThumbnailMissBody is the only 404 body for this route.
var orderThumbnailMissBody = []byte(`{"error":"not_found"}`)

// OrderThumbnail is GET /api/customizer/order-thumbnails/{token}.
// Every failure writes the same 404. The token is not logged.
func OrderThumbnail(open func() (*application.OrderThumbnail, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc, err := open()
		if err != nil || svc == nil {
			writeOrderThumbnailMiss(w)
			return
		}
		body, mime, err := svc.Bytes(r.Context(), r.PathValue("token"))
		if err != nil {
			writeOrderThumbnailMiss(w)
			return
		}
		h := w.Header()
		h.Set("Content-Type", mime)
		h.Set("Content-Disposition", "inline")
		h.Set("Cache-Control", "private, max-age=86400")
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

func writeOrderThumbnailMiss(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(orderThumbnailMissBody)
}
