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
)

const SnapshotOrderJobName = "snapshot_order_design"

// SnapshotOrderDispatchJobName is the order.created job that only enqueues the snapshot.
// RegisterOnEvent cannot set a job id, so this job stays short and the snapshot id is UUID v5.
const SnapshotOrderDispatchJobName = "dispatch_snapshot_order"

// snapshotOrderNamespace is the UUID v5 namespace for snapshot_order_design job ids.
var snapshotOrderNamespace = uuid.MustParse("0e4a0000-0000-4000-8000-0000000000e4")

// SnapshotOrderJobID is the UUID v5 of orderID. Two enqueues of one order share it.
func SnapshotOrderJobID(orderID string) string {
	return uuid.NewSHA1(snapshotOrderNamespace, []byte(strings.TrimSpace(orderID))).String()
}

func registerSnapshotOrder(reg *pluginsdk.SyncJobs, p *Plugin) error {
	if p == nil {
		return fmt.Errorf("metaldogs snapshot: plugin required")
	}
	if err := reg.RegisterManual(SnapshotOrderJobName, p.snapshotOrderHandler()); err != nil {
		return err
	}
	return reg.RegisterOnEvent(SnapshotOrderDispatchJobName, extapi.EventOrderCreated, p.snapshotOrderDispatchHandler())
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
		svc := &application.SnapshotOrder{
			Orders:    p.orderSource,
			Designs:   postgres.NewDesignRepo(db),
			Versions:  postgres.NewDesignVersionRepo(db),
			Templates: postgres.NewTemplateStore(db),
			Snapshots: postgres.NewOrderDesignSnapshotRepo(db),
			Clock:     p.snapshotClock,
		}
		outcomes, err := svc.Execute(ctx, orderID)
		if err != nil {
			line := &application.SnapshotOrderError{OrderID: orderID}
			if se, ok := err.(*application.SnapshotOrderError); ok && se != nil {
				line = se
			}
			logSnapshot(job, "failed", line.OrderID, line.VariantID, line.DesignID, err)
			return err
		}
		for _, line := range outcomes {
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
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("metaldogs snapshot enqueue: order id required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.jobs.EnqueueManual(ctx, SnapshotOrderJobName, SnapshotOrderJobID(orderID), map[string]interface{}{
		"order_id": orderID,
	})
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
