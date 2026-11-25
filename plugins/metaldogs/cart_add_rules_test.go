package metaldogs_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
)

func TestAddToCart_RepeatConflictQuantityAndRemove(t *testing.T) {
	srv, db := newDesignCartServer(t)
	variantM := teeVariantID(t, srv, "MDOGS-TEE-M")
	variantL := teeVariantID(t, srv, "MDOGS-TEE-L")
	approved, token := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 4)
	other, otherToken := insertCartDesign(t, db, domain.DesignApproved, time.Now().UTC().Add(24*time.Hour), 5)
	before := getDesign(t, srv, approved.ID, token)

	status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantM, nil)
	if status != http.StatusOK {
		t.Fatalf("add: status=%d body=%s", status, body)
	}
	cartID := cookieValue(cookies, sessionCartCookie)
	if cartID == "" {
		t.Fatal("session cart cookie missing")
	}
	want := cartItems(t, srv, cartID)
	wantDesign := designIDs(t, db, cartID, want)
	if len(want) != 1 || want[0].Quantity != 1 || wantDesign[variantM] != approved.ID {
		t.Fatalf("items=%+v design=%v", want, wantDesign)
	}

	t.Run("same design same variant", func(t *testing.T) {
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantM, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		assertCartURL(t, body, cartID)
		if got := cookieValue(cookies, sessionCartCookie); got != cartID {
			t.Fatalf("cookie=%q", got)
		}
		assertSameLines(t, db, cartID, want, cartItems(t, srv, cartID), wantDesign)
	})

	t.Run("different design same variant", func(t *testing.T) {
		status, body, _ := addDesignToCart(t, srv, other.ID, otherToken, variantM, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusConflict {
			t.Fatalf("status=%d body=%s", status, body)
		}
		var errBody struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &errBody); err != nil {
			t.Fatal(err)
		}
		if errBody.Error != "conflict" {
			t.Fatalf("error=%q", errBody.Error)
		}
		assertSameLines(t, db, cartID, want, cartItems(t, srv, cartID), wantDesign)
	})

	t.Run("same design other variant", func(t *testing.T) {
		status, body, cookies := addDesignToCart(t, srv, approved.ID, token, variantL, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, body)
		}
		assertCartURL(t, body, cartID)
		if got := cookieValue(cookies, sessionCartCookie); got != cartID {
			t.Fatalf("cookie=%q", got)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 2 {
			t.Fatalf("items=%+v", items)
		}
		if storedDesignID(t, db, cartID, variantM) != approved.ID || lineByVariant(t, items, variantM).Quantity != 1 {
			t.Fatalf("M=%+v", lineByVariant(t, items, variantM))
		}
		if storedDesignID(t, db, cartID, variantL) != approved.ID || lineByVariant(t, items, variantL).Quantity != 1 {
			t.Fatalf("L=%+v", lineByVariant(t, items, variantL))
		}
	})

	t.Run("quantity raised then repeat add", func(t *testing.T) {
		status, body := cartDo(t, srv, http.MethodPut, "/api/v1/carts/"+cartID+"/items/"+variantM, map[string]any{"quantity": 2})
		if status != http.StatusOK {
			t.Fatalf("update: status=%d body=%s", status, body)
		}
		raised := cartItems(t, srv, cartID)
		raisedDesign := designIDs(t, db, cartID, raised)
		if lineByVariant(t, raised, variantM).Quantity != 2 || raisedDesign[variantM] != approved.ID {
			t.Fatalf("after update=%+v design=%v", raised, raisedDesign)
		}
		if lineByVariant(t, raised, variantL).Quantity != 1 {
			t.Fatalf("L changed=%+v", raised)
		}
		status, body, _ = addDesignToCart(t, srv, approved.ID, token, variantM, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("repeat: status=%d body=%s", status, body)
		}
		assertSameLines(t, db, cartID, raised, cartItems(t, srv, cartID), raisedDesign)
	})

	t.Run("remove and add again", func(t *testing.T) {
		status, body := cartDo(t, srv, http.MethodDelete, "/api/v1/carts/"+cartID+"/items/"+variantM, nil)
		if status != http.StatusOK {
			t.Fatalf("delete: status=%d body=%s", status, body)
		}
		left := cartItems(t, srv, cartID)
		if len(left) != 1 || left[0].VariantID != variantL {
			t.Fatalf("after delete=%+v", left)
		}
		status, body, _ = addDesignToCart(t, srv, other.ID, otherToken, variantM, &http.Cookie{Name: sessionCartCookie, Value: cartID})
		if status != http.StatusOK {
			t.Fatalf("re-add: status=%d body=%s", status, body)
		}
		items := cartItems(t, srv, cartID)
		if len(items) != 2 {
			t.Fatalf("items=%+v", items)
		}
		if lineByVariant(t, items, variantM).Quantity != 1 || storedDesignID(t, db, cartID, variantM) != other.ID {
			t.Fatalf("M=%+v", lineByVariant(t, items, variantM))
		}
		if lineByVariant(t, items, variantL).Quantity != 1 || storedDesignID(t, db, cartID, variantL) != approved.ID {
			t.Fatalf("L=%+v", lineByVariant(t, items, variantL))
		}
	})

	assertDesignUnchanged(t, before, getDesign(t, srv, approved.ID, token))
}

func assertCartURL(t *testing.T, body []byte, cartID string) {
	t.Helper()
	var resp struct {
		CartURL string `json:"cart_url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.CartURL != "/cart" {
		t.Fatalf("cart_url=%q", resp.CartURL)
	}
	if strings.Contains(string(body), cartID) || strings.Contains(string(body), "cart_id") {
		t.Fatalf("response exposes cart id: %s", body)
	}
}

func assertSameLines(t *testing.T, db *sql.DB, cartID string, before, after []cartLine, beforeDesign map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("lines %d -> %d\nbefore=%+v\nafter=%+v", len(before), len(after), before, after)
	}
	for _, line := range before {
		got := lineByVariant(t, after, line.VariantID)
		if got.Quantity != line.Quantity || storedDesignID(t, db, cartID, line.VariantID) != beforeDesign[line.VariantID] {
			t.Fatalf("line %s changed\nbefore=%+v\nafter=%+v", line.VariantID, line, got)
		}
	}
}

func lineByVariant(t *testing.T, items []cartLine, variantID string) cartLine {
	t.Helper()
	for _, item := range items {
		if item.VariantID == variantID {
			return item
		}
	}
	t.Fatalf("variant %s missing in %+v", variantID, items)
	return cartLine{}
}
