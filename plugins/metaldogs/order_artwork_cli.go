package metaldogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// orderArtworkOutput is the CLI destination. Nil writes to stdout.
var orderArtworkOutput io.Writer

func registerOrderArtworkCommand(h pluginsdk.Host, p *Plugin) {
	reg := pluginsdk.NewHost(h, PluginName).CLI()
	reg.RegisterCommand(pluginsdk.CLICommand{
		Name:        CommandOrderArtwork,
		Description: "Print snapshot metadata and short-lived signed artwork URLs for an order",
		RequiresDB:  true,
		Run:         p.runOrderArtwork,
	})
}

func (p *Plugin) runOrderArtwork(ctx pluginsdk.CLIContext, args []string) error {
	if p == nil || !p.orderSnapshotEnabled {
		return fmt.Errorf("%s: plugins.metaldogs.order_snapshot.enabled is false", CommandOrderArtwork)
	}
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("%s: order_id required", CommandOrderArtwork)
	}
	db, err := pluginDB(p)
	if err != nil {
		return fmt.Errorf("%s: %w", CommandOrderArtwork, err)
	}
	if p.orderLookup == nil {
		return fmt.Errorf("%s: order source is not bound", CommandOrderArtwork)
	}
	if p.assets == nil {
		return fmt.Errorf("%s: asset store is not configured", CommandOrderArtwork)
	}
	c := ctx.Ctx
	if c == nil {
		c = context.Background()
	}
	var log application.ArtworkLogger
	if ctx.Logger != nil {
		log = ctx.Logger
	}
	clock := p.snapshotClock
	if clock == nil {
		clock = ports.SystemClock{}
	}
	view, err := (&application.OrderArtworkAccess{
		Orders:    p.orderLookup,
		Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
		Assets:    postgres.NewAssetRepo(db),
		Store:     p.assets,
		Log:       postgres.NewArtworkAccessLogRepo(db),
		Clock:     clock,
		Logger:    log,
	}).Access(c, args[0], application.ArtworkActorCLI, ports.ArtworkChannelCLI)
	if err != nil {
		if errors.Is(err, application.ErrOrderNotFound) {
			return fmt.Errorf("not found")
		}
		if errors.Is(err, ports.ErrInvalidOrderID) {
			return fmt.Errorf("%s: order id is not a uuid", CommandOrderArtwork)
		}
		return fmt.Errorf("%s: artwork access failed", CommandOrderArtwork)
	}
	w := orderArtworkOutput
	if w == nil {
		w = os.Stdout
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(view); err != nil {
		return fmt.Errorf("%s: %w", CommandOrderArtwork, err)
	}
	return nil
}
