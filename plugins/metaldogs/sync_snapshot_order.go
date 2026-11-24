package metaldogs

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const SnapshotOrderJobName = "snapshot_order_design"

// SnapshotOrderDispatchJobName is the order.created job that only enqueues the snapshot.
const SnapshotOrderDispatchJobName = "dispatch_snapshot_order"

// snapshotOrderNamespace is the UUID v5 namespace for snapshot_order_design job ids.
var snapshotOrderNamespace = uuid.MustParse("0e4a0000-0000-4000-8000-0000000000e4")

// dispatchOrderNamespace is separate: queue job ids are global, so the dispatcher cannot share the snapshot id.
var dispatchOrderNamespace = uuid.MustParse("0e4b0000-0000-4000-8000-0000000000e4")

// SnapshotOrderJobID is the UUID v5 of orderID. Two enqueues of one order share it.
// A parseable id is hashed in canonical form, so letter case does not split the job.
func SnapshotOrderJobID(orderID string) string {
	id := strings.TrimSpace(orderID)
	if parsed, err := uuid.Parse(id); err == nil {
		id = parsed.String()
	}
	return uuid.NewSHA1(snapshotOrderNamespace, []byte(id)).String()
}

func registerSnapshotOrder(reg *pluginsdk.SyncJobs, p *Plugin) error {
	if p == nil {
		return fmt.Errorf("metaldogs snapshot: plugin required")
	}
	if err := reg.RegisterManual(SnapshotOrderJobName, p.snapshotOrderHandler()); err != nil {
		return err
	}
	return reg.RegisterOnEvent(SnapshotOrderDispatchJobName, extapi.EventOrderCreated, p.snapshotOrderDispatchHandler(), pluginsdk.WithEventJobID(dispatchSnapshotEventJobID))
}

// DispatchSnapshotOrderJobID is the UUID v5 of the dispatcher job type and orderID.
// A blank order id returns "" so enqueue fails closed: NewSyncJobEnvelopeWithID rejects a non-UUID.
func DispatchSnapshotOrderJobID(orderID string) string {
	id := strings.TrimSpace(orderID)
	if id == "" {
		return ""
	}
	if parsed, err := uuid.Parse(id); err == nil {
		id = parsed.String()
	}
	jobType, err := extapi.SyncJobType(PluginName, SnapshotOrderDispatchJobName)
	if err != nil {
		return ""
	}
	return uuid.NewSHA1(dispatchOrderNamespace, []byte(jobType+"\x00"+id)).String()
}

func dispatchSnapshotEventJobID(payload map[string]interface{}) string {
	return DispatchSnapshotOrderJobID(orderIDFromSnapshotPayload(payload))
}

func (p *Plugin) snapshotOrderDispatchHandler() extapi.SyncJobHandler {
	return func(ctx context.Context, job extapi.SyncJobContext) error {
		return p.EnqueueSnapshotOrder(ctx, orderIDFromSnapshotPayload(job.Payload))
	}
}

func (p *Plugin) snapshotOrderHandler() extapi.SyncJobHandler {
	return func(ctx context.Context, job extapi.SyncJobContext) error {
		orderID := orderIDFromSnapshotPayload(job.Payload)
		if p == nil || p.orderSource == nil {
			err := fmt.Errorf("metaldogs snapshot: order source is not bound")
			logSnapshot(job, "failed", orderID, "", "", err)
			return err
		}
		db := job.DB
		if db == nil && p != nil && p.host != nil {
			if bound, ok := p.host.BootstrapDB(); ok {
				db = bound
			}
		}
		if db == nil {
			err := fmt.Errorf("metaldogs snapshot: database required")
			logSnapshot(job, "failed", orderID, "", "", err)
			return err
		}
		var store ports.AssetStore
		if p != nil {
			store = p.assets
		}
		svc := &application.SnapshotOrder{
			Orders:    p.orderSource,
			Designs:   postgres.NewDesignRepo(db),
			Versions:  postgres.NewDesignVersionRepo(db),
			Templates: postgres.NewTemplateStore(db),
			Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
			Mockups:   postgres.NewGenerationJobRepo(db),
			Assets:    postgres.NewAssetRepo(db),
			Store:     store,
			Clock:     p.snapshotClock,
		}
		outcomes, err := svc.Execute(ctx, orderID)
		if err != nil {
			logSnapshotFailure(job, orderID, err)
			return err
		}
		for _, line := range outcomes {
			if line.ThumbnailSkipped {
				warnThumbnail(job, orderID, line.VariantID)
			}
			kind := "duplicate"
			if line.Written {
				kind = "written"
			}
			logSnapshot(job, kind, orderID, line.VariantID, line.DesignID, nil)
		}
		return nil
	}
}

// EnqueueSnapshotOrder queues snapshot_order_design once per order (UUID v5 job id).
func (p *Plugin) EnqueueSnapshotOrder(ctx context.Context, orderID string) error {
	if p == nil || p.jobs == nil {
		return fmt.Errorf("metaldogs snapshot enqueue: plugin not initialised")
	}
	orderID, err := canonicalSnapshotOrderID(orderID)
	if err != nil {
		return fmt.Errorf("metaldogs snapshot enqueue: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.jobs.EnqueueManual(ctx, SnapshotOrderJobName, SnapshotOrderJobID(orderID), map[string]interface{}{
		"order_id": orderID,
	})
}

func canonicalSnapshotOrderID(orderID string) (string, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return "", fmt.Errorf("order id required")
	}
	parsed, err := uuid.Parse(orderID)
	if err != nil {
		return "", ports.ErrInvalidOrderID
	}
	return parsed.String(), nil
}

func orderIDFromSnapshotPayload(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	if id := stringField(payload["order_id"]); id != "" {
		return id
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return ""
	}
	return stringField(data["order_id"])
}

func stringField(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func logSnapshotFailure(job extapi.SyncJobContext, orderID string, err error) {
	se, ok := err.(*application.SnapshotOrderError)
	if !ok || se == nil {
		logSnapshot(job, "failed", orderID, "", "", err)
		return
	}
	if se.OrderID != "" {
		orderID = se.OrderID
	}
	if len(se.FailedLines) > 0 {
		for _, line := range se.FailedLines {
			logSnapshot(job, "failed", orderID, line.VariantID, line.DesignID, err)
		}
		return
	}
	logSnapshot(job, "failed", orderID, se.VariantID, se.DesignID, err)
}

func warnThumbnail(job extapi.SyncJobContext, orderID, variantID string) {
	if job.Logger == nil {
		return
	}
	fields := map[string]interface{}{
		"order_id":   orderID,
		"variant_id": variantID,
	}
	const event = "metaldogs.snapshot_order_design.thumbnail"
	type warner interface {
		Warn(string, map[string]interface{})
	}
	if w, ok := job.Logger.(warner); ok {
		w.Warn(event, fields)
		return
	}
	job.Logger.Info(event, fields)
}

func logSnapshot(job extapi.SyncJobContext, kind, orderID, variantID, designID string, err error) {
	if job.Logger == nil {
		return
	}
	fields := map[string]interface{}{
		"order_id":   orderID,
		"variant_id": variantID,
		"design_id":  designID,
	}
	msg := "metaldogs.snapshot_order_design." + kind
	if err != nil {
		job.Logger.Error(msg, err, fields)
		return
	}
	job.Logger.Info(msg, fields)
}
