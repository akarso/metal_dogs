package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/localfs"
)

// DevAssets serves GET /api/customizer/dev-assets/{token} for the local
// AssetStore HMAC shim (ADR-0004). Not registered when using SignedURLStorage.
func DevAssets(store *localfs.AssetStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if store == nil {
			http.NotFound(w, r)
			return
		}
		token := r.PathValue("token")
		if token == "" {
			token = strings.TrimPrefix(r.URL.Path, localfs.DevAssetsPath)
			token = strings.Trim(token, "/")
		}
		if token == "" || strings.Contains(token, "/") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		key, err := store.ParseToken(token)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f, err := store.Open(key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
	})
}
