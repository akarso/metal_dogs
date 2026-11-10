package storefront

import (
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AuthMiddleware parses the Authorization header and injects an Identity
// into the request context. If no token is present, a guest identity is
// injected. If the token is invalid, a 401 response is returned.
//
// Paths /api/customizer and /api/customizer/* use Authorization: Bearer for
// Metal Dogs owner capability tokens (ADR-0007), which are not Shopanda JWTs.
// Bearer on those paths is left opaque for plugin handlers; identity still
// comes from the storefront session cookie when present, otherwise guest.
func AuthMiddleware(parser auth.TokenParser) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			// Customizer owner tokens must not be JWT-parsed (raw base64url → 401).
			if isCustomizerPath(r.URL.Path) {
				if _, ok := bearerTokenValue(header); ok {
					id := identity.Guest()
					if token := storefrontSessionToken(r); token != "" {
						if parsed, err := parser.Parse(r.Context(), token); err == nil {
							id = parsed
						}
					}
					ctx := auth.WithIdentity(r.Context(), id)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			if header == "" {
				if token := storefrontSessionToken(r); token != "" {
					id, err := parser.Parse(r.Context(), token)
					if err == nil {
						ctx := auth.WithIdentity(r.Context(), id)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
				if existing := auth.IdentityFrom(r.Context()); !existing.IsGuest() {
					next.ServeHTTP(w, r)
					return
				}
				ctx := auth.WithIdentity(r.Context(), identity.Guest())
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token, ok := bearerTokenValue(header)
			if !ok {
				httpshared.JSONError(w, apperror.Unauthorized("invalid authorization header"))
				return
			}

			id, err := parser.Parse(r.Context(), token)
			if err != nil {
				httpshared.JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}

			ctx := auth.WithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isCustomizerPath(path string) bool {
	return path == "/api/customizer" || strings.HasPrefix(path, "/api/customizer/")
}

func bearerTokenValue(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// RequireAuth rejects unauthenticated (guest) requests with a 401 response.
func RequireAuth() httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				httpshared.JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
