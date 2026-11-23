package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/hostorders"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func (p *Plugin) openOrderArtwork() (*application.OrderArtworkAccess, error) {
	db, err := pluginDB(p)
	if err != nil {
		return nil, err
	}
	lookup, err := p.resolveOrderLookup()
	if err != nil {
		return nil, err
	}
	if p.assets == nil {
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
		Orders:    lookup,
		Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
		Assets:    postgres.NewAssetRepo(db),
		Store:     p.assets,
		Log:       postgres.NewArtworkAccessLogRepo(db),
		Clock:     clock,
		Logger:    log,
	}, nil
}

// resolveOrderLookup reads Host.Orders on each call. The plugin CLI root runs
// InitAll before BindOrdersPort, so a lookup captured in Init stays nil.
func (p *Plugin) resolveOrderLookup() (ports.OrderLookup, error) {
	if p != nil && p.orderLookup != nil {
		return p.orderLookup, nil
	}
	var orders interface {
		Orders() (pluginsdk.OrderPort, bool)
	}
	if p != nil {
		orders, _ = p.host.(interface {
			Orders() (pluginsdk.OrderPort, bool)
		})
	}
	src := hostorders.FromHost(orders)
	lookup, ok := src.(ports.OrderLookup)
	if !ok || lookup == nil {
		return nil, fmt.Errorf("metaldogs: order source is not bound")
	}
	return lookup, nil
}
