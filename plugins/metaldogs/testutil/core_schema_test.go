package testutil_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/plugins/metaldogs/testutil"
)

func TestOpenIsolatedWithCore_SkipsWithoutDSN(t *testing.T) {
	t.Setenv("SHOPANDA_TEST_DSN", "")
	testutil.OpenIsolatedWithCore(t, "no_dsn")
	t.Fatal("expected skip when SHOPANDA_TEST_DSN is unset")
}

func TestOpenIsolatedWithCore_StorefrontEmptyProductList(t *testing.T) {
	db := testutil.OpenIsolatedWithCore(t, "storefront_empty")

	repo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	h := storefront.NewProductHandler(
		repo,
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		nil,
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products", h.List())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/products")
	if err != nil {
		t.Fatalf("GET /api/v1/products: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var envelope struct {
		Data struct {
			Products json.RawMessage `json:"products"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		t.Fatalf("error = %s", envelope.Error)
	}
	var products []json.RawMessage
	if err := json.Unmarshal(envelope.Data.Products, &products); err != nil {
		t.Fatalf("products: %v raw=%s", err, envelope.Data.Products)
	}
	if len(products) != 0 {
		t.Fatalf("products = %d, want 0", len(products))
	}
}

func TestOpenIsolatedWithCore_CleanupDropsSchema(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}

	var schema string
	t.Run("inner", func(t *testing.T) {
		db := testutil.OpenIsolatedWithCore(t, "core_cleanup")
		if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
			t.Fatalf("current_schema: %v", err)
		}
		if schema == "" || schema == "public" {
			t.Fatalf("unexpected schema %q", schema)
		}
		var products bool
		if err := db.QueryRow(`SELECT to_regclass('products') IS NOT NULL`).Scan(&products); err != nil {
			t.Fatalf("products table: %v", err)
		}
		if !products {
			t.Fatal("core migrations did not create products")
		}
		var meta bool
		if err := db.QueryRow(`SELECT to_regclass('metaldogs_meta') IS NOT NULL`).Scan(&meta); err != nil {
			t.Fatalf("metaldogs_meta: %v", err)
		}
		if !meta {
			t.Fatal("plugin migrations did not create metaldogs_meta")
		}
	})

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer admin.Close()
	var exists bool
	err = admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema).Scan(&exists)
	if err != nil {
		t.Fatalf("pg_namespace: %v", err)
	}
	if exists {
		t.Fatalf("schema %q still exists after OpenIsolatedWithCore cleanup", schema)
	}
}
