package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// shopanda_storefront_cart is unexported in the storefront; the name must match or /cart ignores the line.
const (
	sessionCartCookie       = "shopanda_storefront_cart"
	sessionCartCookieMaxAge = 60 * 60 * 24 * 30
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
		variantID := strings.TrimSpace(body.VariantID)
		if variantID == "" || !looksLikeUUID(variantID) {
			writeJSONError(w, http.StatusUnprocessableEntity, application.DesignErrUnprocessable)
			return
		}
		next, cartURL, err := add.Execute(r.Context(), sessionCartID(r), id, token, variantID)
		if err != nil {
			writeDesignError(w, err)
			return
		}
		setSessionCartCookie(w, r, next)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(addToCartResponse{CartURL: cartURL})
	})
}

func sessionCartID(r *http.Request) ports.CartSession {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(sessionCartCookie)
	if err != nil {
		return ""
	}
	return ports.CartSession(strings.TrimSpace(cookie.Value))
}

func setSessionCartCookie(w http.ResponseWriter, r *http.Request, cartID ports.CartSession) {
	if w == nil || strings.TrimSpace(string(cartID)) == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCartCookie,
		Value:    string(cartID),
		Path:     "/",
		MaxAge:   sessionCartCookieMaxAge,
		HttpOnly: true,
		Secure:   r != nil && r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}
