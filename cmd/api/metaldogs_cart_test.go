package main

import (
	"context"
	"database/sql"
	"io"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domainPricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestBuildServeHandler_BindsMetalDogsSessionCart(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	registerMetalDogs(reg)
	var bound *metaldogs.Plugin
	for _, e := range reg.Entries() {
		if p, ok := e.Plugin.(*metaldogs.Plugin); ok {
			bound = p
		}
	}
	if bound == nil {
		t.Fatal("metaldogs missing")
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
	func() {
		defer func() { _ = recover() }()
		_, _ = buildServeHandler(&config.Config{}, log, rt, db, context.Background())
	}()
	if !bound.SessionCartWired() {
		t.Fatal("composition root did not supply the session cart port")
	}
}
