package metaldogs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestCartValidation_TeeWithoutDesignWhenMediaUnavailable(t *testing.T) {
	srv, _, plugin := newDesignCartServerWithoutMedia(t)
	if plugin.DesignService() != nil {
		t.Fatal("design service wired without media")
	}
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
	cartID := createPLNCart(t, srv)
	status, body := cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
		"variant_id": variantM,
		"quantity":   1,
	})
	if status != http.StatusUnprocessableEntity || cartErrorCode(t, body) != "cart_validation_failed" {
		t.Fatalf("status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
	}
	issues := validationIssuesFrom(t, body)
	if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignRequiredCode, "design required", variantM) {
		t.Fatalf("issues=%+v", issues)
	}
	if items := cartItems(t, srv, cartID); len(items) != 0 {
		t.Fatalf("stored items=%+v", items)
	}
}

func TestCartValidation_DesignLines(t *testing.T) {
	srv, db := newDesignCartServer(t)
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
	variantL := teeVariantID(t, srv, "MDOGS-TEE-L")
	otherVariant := insertPublishedOtherVariant(t, db)

	t.Run("valid design has no issue without a token", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 4)
		cartID := addDesignLine(t, srv, design.ID, token, variantM)
		if issues := cartValidationIssues(t, srv, cartID); len(issues) != 0 {
			t.Fatalf("issues=%+v", issues)
		}
		status, body := cartDo(t, srv, http.MethodPut, "/api/v1/carts/"+cartID+"/items/"+variantM, map[string]any{"quantity": 2})
		if status != http.StatusOK {
			t.Fatalf("quantity: status=%d body=%s", status, body)
		}
		if issues := cartValidationIssues(t, srv, cartID); len(issues) != 0 {
			t.Fatalf("issues after quantity=%+v", issues)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 1 || items[0].Quantity != 2 || extensionValue(t, items[0]) != design.ID {
			t.Fatalf("items=%+v", items)
		}
	})

	t.Run("custom tee without design", func(t *testing.T) {
		cartID := createPLNCart(t, srv)
		status, body := cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": variantM,
			"quantity":   1,
		})
		if status != http.StatusUnprocessableEntity || cartErrorCode(t, body) != "cart_validation_failed" {
			t.Fatalf("status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
		}
		issues := validationIssuesFrom(t, body)
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignRequiredCode, "design required", variantM) {
			t.Fatalf("issues=%+v", issues)
		}
		if items := cartItems(t, srv, cartID); len(items) != 0 {
			t.Fatalf("stored items=%+v", items)
		}
	})

	t.Run("other product without design", func(t *testing.T) {
		cartID := createPLNCart(t, srv)
		status, body := cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": otherVariant,
			"quantity":   1,
		})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		if issues := cartValidationIssues(t, srv, cartID); len(issues) != 0 {
			t.Fatalf("issues=%+v", issues)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 1 || items[0].VariantID != otherVariant || len(items[0].Extensions) != 0 {
			t.Fatalf("items=%+v", items)
		}
	})

	t.Run("design no longer approved", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 6)
		cartID := addDesignLine(t, srv, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET status = 'ordered' WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		issues := cartValidationIssues(t, srv, cartID)
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignNotApprovedCode, "design is not approved", variantM) {
			t.Fatalf("issues=%+v", issues)
		}
		status, body := cartDo(t, srv, http.MethodPut, "/api/v1/carts/"+cartID+"/items/"+variantM, map[string]any{"quantity": 3})
		if status != http.StatusUnprocessableEntity || cartErrorCode(t, body) != "cart_validation_failed" {
			t.Fatalf("status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
		}
		if items := cartItems(t, srv, cartID); len(items) != 1 || items[0].Quantity != 1 {
			t.Fatalf("quantity changed: %+v", items)
		}
	})

	t.Run("design expired", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 7)
		cartID := addDesignLine(t, srv, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET expires_at = now() - interval '1 hour' WHERE id = $1`, design.ID); err != nil {
			t.Fatal(err)
		}
		issues := cartValidationIssues(t, srv, cartID)
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignExpiredCode, "design has expired", variantM) {
			t.Fatalf("issues=%+v", issues)
		}
	})

	t.Run("unknown design id", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 8)
		cartID := addDesignLine(t, srv, design.ID, token, variantM)
		if _, err := db.ExecContext(context.Background(), `
			UPDATE extension_values
			SET value = jsonb_set(value, '{string_value}', to_jsonb($1::text))
			WHERE field_code = 'metaldogs.design_id' AND value->>'string_value' = $2
		`, id.New(), design.ID); err != nil {
			t.Fatal(err)
		}
		issues := cartValidationIssues(t, srv, cartID)
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignNotFoundCode, "design not found", variantM) {
			t.Fatalf("issues=%+v", issues)
		}
	})

	t.Run("failed add does not leave a design for a generic line", func(t *testing.T) {
		design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 13)
		cartID := createPLNCart(t, srv)
		status, body := cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": otherVariant,
			"quantity":   1,
		})
		if status != http.StatusOK {
			t.Fatalf("other product: status=%d body=%s", status, body)
		}
		setVariantStock(t, db, variantM, 0)
		t.Cleanup(func() { setVariantStock(t, db, variantM, 5) })
		status, body, _ = addDesignToCart(t, srv, design.ID, token, variantM, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("add: status=%d body=%s", status, body)
		}
		setVariantStock(t, db, variantM, 5)
		status, body = cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": variantM,
			"quantity":   1,
		})
		if status != http.StatusUnprocessableEntity || cartErrorCode(t, body) != "cart_validation_failed" {
			t.Fatalf("status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
		}
		issues := validationIssuesFrom(t, body)
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignRequiredCode, "design required", variantM) {
			t.Fatalf("issues=%+v", issues)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 1 || items[0].VariantID != otherVariant {
			t.Fatalf("items=%+v", items)
		}
	})

	t.Run("stale line does not strip another design on remove", func(t *testing.T) {
		good, goodToken := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 11)
		stale, staleToken := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 12)
		cartID := addDesignLine(t, srv, good.ID, goodToken, variantM)
		status, body, _ := addDesignToCart(t, srv, stale.ID, staleToken, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("add L: status=%d body=%s", status, body)
		}
		if _, err := db.ExecContext(context.Background(), `UPDATE metaldogs_designs SET expires_at = now() - interval '1 hour' WHERE id = $1`, stale.ID); err != nil {
			t.Fatal(err)
		}
		status, body = cartDo(t, srv, http.MethodPut, "/api/v1/carts/"+cartID+"/items/"+variantM, map[string]any{"quantity": 2})
		if status != http.StatusOK {
			t.Fatalf("quantity: status=%d body=%s", status, body)
		}
		status, body = cartDo(t, srv, http.MethodPost, "/api/v1/carts/"+cartID+"/items", map[string]any{
			"variant_id": otherVariant,
			"quantity":   1,
		})
		if status != http.StatusOK {
			t.Fatalf("other product: status=%d body=%s", status, body)
		}
		items := cartItems(t, srv, cartID)
		if extensionValue(t, lineByVariant(t, items, variantM)) != good.ID || lineByVariant(t, items, variantM).Quantity != 2 {
			t.Fatalf("M=%+v", lineByVariant(t, items, variantM))
		}
		if extensionValue(t, lineByVariant(t, items, variantL)) != stale.ID {
			t.Fatalf("L=%+v", lineByVariant(t, items, variantL))
		}
		status, body = cartDo(t, srv, http.MethodPut, "/api/v1/carts/"+cartID+"/items/"+otherVariant, map[string]any{"quantity": 1})
		if status != http.StatusUnprocessableEntity || cartErrorCode(t, body) != "cart_validation_failed" {
			t.Fatalf("same quantity: status=%d code=%s body=%s", status, cartErrorCode(t, body), body)
		}
		if lineByVariant(t, cartItems(t, srv, cartID), otherVariant).Quantity != 1 {
			t.Fatal("same-quantity write changed the other line")
		}
		status, body = cartDo(t, srv, http.MethodDelete, "/api/v1/carts/"+cartID+"/items/"+variantM, nil)
		if status != http.StatusOK {
			t.Fatalf("delete: status=%d body=%s", status, body)
		}
		items = cartItems(t, srv, cartID)
		if len(items) != 2 {
			t.Fatalf("items=%+v", items)
		}
		for _, item := range items {
			if item.VariantID == variantM {
				t.Fatalf("removed line still present: %+v", items)
			}
		}
		if extensionValue(t, lineByVariant(t, items, variantL)) != stale.ID {
			t.Fatalf("L lost design: %+v", lineByVariant(t, items, variantL))
		}
		issues := cartValidationIssues(t, srv, cartID)
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignExpiredCode, "design has expired", variantL) {
			t.Fatalf("issues=%+v", issues)
		}
	})
}

func TestAddToCart_ConcurrentSameDesignKeepsDesignID(t *testing.T) {
	srv, db := newDesignCartServer(t)
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
	design, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 14)
	cartID := createPLNCart(t, srv)
	cookie := &http.Cookie{Name: sessionCartCookie, Value: cartID}

	const n = 8
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			raw, err := json.Marshal(map[string]string{"variant_id": variantM})
			if err != nil {
				errs <- err
				return
			}
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/customizer/designs/"+design.ID+"/add-to-cart", bytes.NewReader(raw))
			if err != nil {
				errs <- err
				return
			}
			req.Host = catalogPLNHost
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookie)
			resp, err := srv.Client().Do(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	items := cartItems(t, srv, cartID)
	if len(items) != 1 || items[0].VariantID != variantM || extensionValue(t, items[0]) != design.ID {
		t.Fatalf("items=%+v", items)
	}
}

func addDesignLine(t *testing.T, srv *httptest.Server, designID, token, variantID string) string {
	t.Helper()
	status, body, cookies := addDesignToCart(t, srv, designID, token, variantID, nil)
	if status != http.StatusOK {
		t.Fatalf("add: status=%d body=%s", status, body)
	}
	cartID := cookieValue(cookies, sessionCartCookie)
	if cartID == "" {
		t.Fatal("session cart cookie missing")
	}
	return cartID
}

type cartValidationIssue struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	VariantID string `json:"variant_id"`
	Level     string `json:"level"`
}

func cartValidationIssues(t *testing.T, srv *httptest.Server, cartID string) []cartValidationIssue {
	t.Helper()
	status, body := cartDo(t, srv, http.MethodGet, "/api/v1/carts/"+cartID, nil)
	if status != http.StatusOK {
		t.Fatalf("get cart: status=%d body=%s", status, body)
	}
	return validationIssuesFrom(t, body)
}

func validationIssuesFrom(t *testing.T, body []byte) []cartValidationIssue {
	t.Helper()
	var envelope struct {
		Data struct {
			ValidationErrors []cartValidationIssue `json:"validation_errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("issues: %v body=%s", err, body)
	}
	return envelope.Data.ValidationErrors
}

func blockingDesignIssue(issue cartValidationIssue, code, message, variantID string) bool {
	return issue.Code == code && issue.Message == message && issue.VariantID == variantID && issue.Level != "warning"
}
