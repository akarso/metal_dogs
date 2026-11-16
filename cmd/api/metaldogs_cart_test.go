package main

import (
	"bytes"
	"database/sql"
	"io"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domainPricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestBindMetalDogsSessionCart_MissingDependencyWarns(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, "warn")
	bindMetalDogsSessionCart(&serveRuntime{log: log})
	if !strings.Contains(buf.String(), "metaldogs: session cart bind skipped") {
		t.Fatalf("log=%s", buf.String())
	}
	buf.Reset()
	bindMetalDogsSessionCart(nil)
	if buf.Len() != 0 {
		t.Fatalf("nil runtime logged %s", buf.String())
	}
}

func TestBindMetalDogsSessionCart(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	staleReg := plugin.NewRegistry(log)
	registerMetalDogs(staleReg)
	stale := metalDogsFromRegistry(t, staleReg)
	reg := plugin.NewRegistry(log)
	registerMetalDogs(reg)
	bound := metalDogsFromRegistry(t, reg)
	if bound.SessionCartWired() {
		t.Fatal("session cart wired before bind")
	}
	db, err := sql.Open("pgx", "postgres://shopanda:changeme@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repos, err := openServeRepos(db)
	if err != nil {
		t.Fatal(err)
	}
	cartSvc := cartApp.NewService(
		repos.cartRepo, repos.priceRepo, nil, nil,
		repos.variantRepo, repos.productRepo, repos.bundleComponentRepo, repos.stockRepo,
		domainPricing.NewPipeline(domainPricing.NewFinalizeStep()),
		log, event.NewBus(log), nil, nil,
	)
	rt := &serveRuntime{cartService: cartSvc, repos: repos, log: log}
	bindMetalDogsSessionCart(rt)
	if !bound.SessionCartWired() {
		t.Fatal("composition root did not supply the session cart port")
	}
	if stale.SessionCartWired() {
		t.Fatal("session cart port was bound to an earlier plugin instance")
	}
}

func metalDogsFromRegistry(t *testing.T, reg *plugin.Registry) *metaldogs.Plugin {
	t.Helper()
	for _, e := range reg.Entries() {
		if p, ok := e.Plugin.(*metaldogs.Plugin); ok {
			return p
		}
	}
	t.Fatal("metaldogs missing")
	return nil
}
