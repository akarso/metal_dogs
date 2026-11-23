package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func (p *Plugin) openOrderArtwork() (*application.OrderArtworkAccess, error) {
	db, err := pluginDB(p)
	if err != nil {
		return nil, err
	}
	if p.orderLookup == nil || p.assets == nil {
		return nil, fmt.Errorf("metaldogs: order artwork is not configured")
	}
	var log application.ArtworkLogger
	if p.host != nil && p.host.Logger() != nil {
		log = p.host.Logger()
	}
	clock := p.snapshotClock
	if clock == nil {
		clock = ports.SystemClock{}
	}
	return &application.OrderArtworkAccess{
		Orders:    p.orderLookup,
		Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
		Assets:    postgres.NewAssetRepo(db),
		Store:     p.assets,
		Log:       postgres.NewArtworkAccessLogRepo(db),
		Clock:     clock,
		Logger:    log,
	}, nil
}
