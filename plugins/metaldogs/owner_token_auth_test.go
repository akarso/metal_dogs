package metaldogs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appauth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	platformauth "github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/jwt/jwttest"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

type nopCustomers struct{}

func (nopCustomers) FindByID(context.Context, string) (*customer.Customer, error) { return nil, nil }
func (nopCustomers) FindByEmail(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (nopCustomers) Create(context.Context, *customer.Customer) error { return nil }
func (nopCustomers) Update(context.Context, *customer.Customer) error { return nil }
func (nopCustomers) ListCustomers(context.Context, int, int) ([]customer.Customer, error) {
	return nil, nil
}
func (nopCustomers) BumpTokenGeneration(context.Context, string) error { return nil }
func (nopCustomers) ChangePasswordAndBumpTokenGeneration(context.Context, string, string) error {
	return nil
}
func (nopCustomers) Delete(context.Context, string) error { return nil }

func TestAuthMiddleware_DotlessOwnerTokenIsGuest(t *testing.T) {
	issuer, err := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	parser := appauth.NewValidatingTokenParser(issuer, nopCustomers{}, 0)
	token, _, err := application.MintOwnerToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || strings.Contains(token, ".") {
		t.Fatalf("owner token = %q, want dotless base64url", token)
	}

	var got identity.Identity
	handler := storefront.AuthMiddleware(parser)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = platformauth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/x/add-to-cart", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (guest, not 401)", rec.Code)
	}
	if !got.IsGuest() {
		t.Fatalf("identity=%+v, want guest", got)
	}
}
