package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

type addToCartBody struct {
	VariantID string `json:"variant_id"`
}

type addToCartResponse struct {
	CartURL string `json:"cart_url"`
}

// AddDesignToCart handles POST /api/customizer/designs/{id}/add-to-cart.
func AddDesignToCart(add *application.AddToCart) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if add == nil {
			writeJSONError(w, http.StatusServiceUnavailable, application.DesignErrUnavailable)
			return
		}
		if tokenInQuery(r) {
			writeJSONError(w, http.StatusBadRequest, application.DesignErrBadRequest)
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || !looksLikeUUID(id) {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		token, ok := bearerToken(r)
		if !ok {
			writeJSONError(w, http.StatusNotFound, application.DesignErrNotFound)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var body addToCartBody
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, application.DesignErrUnprocessable)
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeJSONError(w, http.StatusUnprocessableEntity, application.DesignErrUnprocessable)
			return
		}
		variantID := strings.TrimSpace(body.VariantID)
		if variantID == "" || !looksLikeUUID(variantID) {
			writeJSONError(w, http.StatusUnprocessableEntity, application.DesignErrUnprocessable)
			return
		}
		cartURL, err := add.Execute(r.Context(), w, r, id, token, variantID)
		if err != nil {
			writeDesignError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(addToCartResponse{CartURL: cartURL})
	})
}
