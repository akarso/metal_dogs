package httpapi

import (
	"errors"
	"net/http"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

type orderThumbnailLog interface {
	Warn(event string, fields map[string]interface{})
}

// orderThumbnailMissBody is the only 404 body for this route.
var orderThumbnailMissBody = []byte(`{"error":"not_found"}`)

// OrderThumbnail is GET /api/customizer/order-thumbnails/{token}.
// Every failure writes the same 404. A read error is logged with order id and variant id, never the token.
func OrderThumbnail(open func() (*application.OrderThumbnail, error), log orderThumbnailLog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc, err := open()
		if err != nil || svc == nil {
			writeOrderThumbnailMiss(w)
			return
		}
		body, mime, err := svc.Bytes(r.Context(), r.PathValue("token"))
		if err != nil {
			warnOrderThumbnail(log, err)
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

func warnOrderThumbnail(log orderThumbnailLog, err error) {
	var fault *application.OrderThumbnailFault
	if log == nil || !errors.As(err, &fault) || fault == nil {
		return
	}
	log.Warn("metaldogs.order_thumbnail", map[string]interface{}{
		"order_id":   fault.OrderID,
		"variant_id": fault.VariantID,
		"cause":      fault.Cause,
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
