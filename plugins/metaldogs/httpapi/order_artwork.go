package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const (
	artworkInvalidOrderCode = "plugin.metaldogs.invalid_order_id"
	artworkNotFoundCode     = "plugin.metaldogs.order_not_found"
	artworkUnavailableCode  = "plugin.metaldogs.artwork_unavailable"
)

// OrderArtwork is GET /api/v1/admin/orders/{order_id}/artwork.
// Actor is pluginsdk.CustomerID. RequirePermission on the admin route returns 401 for guests before this handler runs.
func OrderArtwork(open func() (*application.OrderArtworkAccess, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := pluginsdk.CustomerID(r)
		svc, err := open()
		if err != nil || svc == nil {
			writeArtworkError(w, http.StatusServiceUnavailable, artworkUnavailableCode, "order artwork access failed")
			return
		}
		view, err := svc.Access(r.Context(), strings.TrimSpace(r.PathValue("order_id")), actor, ports.ArtworkChannelAdmin)
		if err != nil {
			writeArtworkAccessError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(view)
	})
}

func writeArtworkAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ports.ErrInvalidOrderID):
		writeArtworkError(w, http.StatusBadRequest, artworkInvalidOrderCode, "order id is not a uuid")
	case errors.Is(err, application.ErrOrderNotFound):
		writeArtworkError(w, http.StatusNotFound, artworkNotFoundCode, "not found")
	default:
		writeArtworkError(w, http.StatusServiceUnavailable, artworkUnavailableCode, "order artwork access failed")
	}
}

func writeArtworkError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": nil,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
