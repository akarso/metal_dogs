package metaldogs

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const ReconcileSnapshotsJobName = "reconcile_snapshots"

const reconcileSnapshotsCron = "@every 5m"

// reconcileOutput is the CLI report destination. Nil writes to stdout.
var reconcileOutput io.Writer

func registerReconcileSnapshots(reg *pluginsdk.SyncJobs, p *Plugin) error {
	if p == nil {
		return fmt.Errorf("metaldogs reconcile: plugin required")
	}
	return reg.RegisterCron(ReconcileSnapshotsJobName, reconcileSnapshotsCron, p.reconcileSnapshotsHandler())
}

func (p *Plugin) reconcileSnapshotsHandler() extapi.SyncJobHandler {
	return func(ctx context.Context, job extapi.SyncJobContext) error {
		db := job.DB
		if db == nil && p != nil && p.host != nil {
			if bound, ok := p.host.BootstrapDB(); ok {
				db = bound
			}
		}
		gaps, err := p.reconcileOrders(ctx, db, "", true)
		if err != nil {
			if job.Logger != nil {
				job.Logger.Error("metaldogs.reconcile_snapshots.failed", err, nil)
			}
			return err
		}
		if job.Logger == nil {
			return nil
		}
		for _, gap := range gaps {
			job.Logger.Info("metaldogs.reconcile_snapshots.gap", map[string]interface{}{
				"order_id": gap.OrderID,
				"line_key": gap.LineKey,
				"reason":   gap.Reason,
			})
		}
		return nil
	}
}

func (p *Plugin) reconcileOrders(ctx context.Context, db *sql.DB, orderID string, fill bool) ([]application.ReconcileGap, error) {
	if p == nil || p.orderSource == nil {
		return nil, fmt.Errorf("metaldogs reconcile: order source is not bound")
	}
	if db == nil {
		return nil, fmt.Errorf("metaldogs reconcile: database required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	svc := &application.ReconcileSnapshots{
		Orders:     p.orderSource,
		Snapshots:  postgres.NewOrderDesignSnapshotRepo(db),
		Versions:   postgres.NewDesignVersionRepo(db),
		JobFailure: p.snapshotJobFailure,
		Fill:       fill,
	}
	return svc.Run(ctx, orderID)
}

func (p *Plugin) snapshotJobFailure(ctx context.Context, orderID string) (bool, string, error) {
	if p == nil || p.jobs == nil {
		return false, "", nil
	}
	store, ok := p.jobs.Store()
	if !ok || store == nil {
		return false, "", nil
	}
	detail, err := store.Get(ctx, SnapshotOrderJobID(orderID))
	if err != nil {
		return false, "", err
	}
	if detail == nil || detail.Status != ports.JobStatusFailed {
		return false, "", nil
	}
	reason := strings.TrimSpace(detail.LastError)
	if reason == "" {
		return true, "snapshot_job_failed", nil
	}
	return true, "snapshot_job_failed: " + reason, nil
}

// FillSnapshotProduction records ready production artwork on snapshots of versionID.
// A version that is not ready, or a second call, changes nothing.
func (p *Plugin) FillSnapshotProduction(ctx context.Context, versionID string) error {
	db, err := pluginDB(p)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return (&application.FillSnapshotProduction{
		Versions:  postgres.NewDesignVersionRepo(db),
		Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
	}).Execute(ctx, versionID)
}

func pluginDB(p *Plugin) (*sql.DB, error) {
	if p == nil || p.host == nil {
		return nil, fmt.Errorf("metaldogs: database is not configured")
	}
	conn, ok := p.host.BootstrapDB()
	if !ok || conn == nil {
		return nil, fmt.Errorf("metaldogs: database is not configured")
	}
	return conn, nil
}

func (p *Plugin) runSnapshots(ctx pluginsdk.CLIContext, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: subcommand required (reconcile)", CommandSnapshots)
	}
	switch args[0] {
	case "reconcile":
		return p.runReconcileCLI(ctx, args[1:])
	default:
		return fmt.Errorf("%s: unknown subcommand %q (want reconcile)", CommandSnapshots, args[0])
	}
}

func (p *Plugin) runReconcileCLI(ctx pluginsdk.CLIContext, args []string) error {
	fs := flag.NewFlagSet(CommandSnapshots+" reconcile", flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())
	orderID := fs.String("order", "", "limit the report to one order id")
	fix := fs.Bool("fix", false, "enqueue snapshot_order_design for each affected order")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c := ctx.Ctx
	if c == nil {
		c = context.Background()
	}
	db, err := pluginDB(p)
	if err != nil {
		return fmt.Errorf("%s reconcile: %w", CommandSnapshots, err)
	}
	gaps, err := p.reconcileOrders(c, db, *orderID, p != nil && p.orderSnapshotEnabled)
	if err != nil {
		return fmt.Errorf("%s reconcile: %w", CommandSnapshots, err)
	}
	writeReconcileReport(gaps)
	if !*fix {
		return nil
	}
	if p == nil || !p.orderSnapshotEnabled {
		return fmt.Errorf("%s reconcile: --fix refused while plugins.metaldogs.order_snapshot.enabled is false", CommandSnapshots)
	}
	seen := map[string]struct{}{}
	for _, gap := range gaps {
		if _, ok := seen[gap.OrderID]; ok {
			continue
		}
		seen[gap.OrderID] = struct{}{}
		if err := p.EnqueueSnapshotOrder(c, gap.OrderID); err != nil {
			return fmt.Errorf("%s reconcile: %w", CommandSnapshots, err)
		}
	}
	return nil
}

func writeReconcileReport(gaps []application.ReconcileGap) {
	w := reconcileOutput
	if w == nil {
		w = os.Stdout
	}
	for _, gap := range gaps {
		fmt.Fprintf(w, "order_id=%s line_key=%s reason=%s\n", gap.OrderID, gap.LineKey, gap.Reason)
	}
}
