package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs"
	"github.com/akarso/shopanda/plugins/metaldogs/catalog"
)

func registerMetalDogs(registry *plugin.Registry) {
	p := metaldogs.New()
	p.UseCatalogRepos(metalDogsCatalogRepos)
	registry.Register(p)
}

func metalDogsCatalogRepos(db *sql.DB) (catalog.Repos, error) {
	products, err := postgres.NewProductRepo(db)
	if err != nil {
		return catalog.Repos{}, fmt.Errorf("metaldogs:catalog-seed: product repo: %w", err)
	}
	variants, err := postgres.NewVariantRepo(db)
	if err != nil {
		return catalog.Repos{}, fmt.Errorf("metaldogs:catalog-seed: variant repo: %w", err)
	}
	prices, err := postgres.NewPriceRepo(db)
	if err != nil {
		return catalog.Repos{}, fmt.Errorf("metaldogs:catalog-seed: price repo: %w", err)
	}
	history, err := postgres.NewPriceHistoryRepo(db)
	if err != nil {
		return catalog.Repos{}, fmt.Errorf("metaldogs:catalog-seed: price history repo: %w", err)
	}
	stock, err := postgres.NewStockRepo(db)
	if err != nil {
		return catalog.Repos{}, fmt.Errorf("metaldogs:catalog-seed: stock repo: %w", err)
	}
	return catalog.Repos{
		Products: products,
		Variants: variants,
		Prices:   prices,
		History:  history,
		Stock:    stock,
		DB:       db,
	}, nil
}
