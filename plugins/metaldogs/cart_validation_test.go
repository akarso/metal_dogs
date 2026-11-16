package metaldogs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestCartValidation_DesignLines(t *testing.T) {
	srv, db := newDesignCartServer(t)
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
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

	t.Run("design id does not match the custom tee", func(t *testing.T) {
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
		if len(issues) != 1 || !blockingDesignIssue(issues[0], ports.DesignMismatchCode, "design does not match the custom tee", variantM) {
			t.Fatalf("issues=%+v", issues)
		}
	})
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
