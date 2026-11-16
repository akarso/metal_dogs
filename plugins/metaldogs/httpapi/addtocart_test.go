package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/httpapi"
)

func TestAddDesignToCart_BodyLimit(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	body := `{"variant_id":"` + id + `"` + strings.Repeat(" ", 1<<20) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+id+"/add-to-cart", strings.NewReader(body))
	req.SetPathValue("id", id)
	req.Header.Set("Authorization", "Bearer owner-token")
	rec := httptest.NewRecorder()
	httpapi.AddDesignToCart(&application.AddToCart{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAddDesignToCart_RejectsTrailingJSON(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	body := `{"variant_id":"` + id + `"} śmieci`
	req := httptest.NewRequest(http.MethodPost, "/api/customizer/designs/"+id+"/add-to-cart", strings.NewReader(body))
	req.SetPathValue("id", id)
	req.Header.Set("Authorization", "Bearer owner-token")
	rec := httptest.NewRecorder()
	httpapi.AddDesignToCart(&application.AddToCart{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
