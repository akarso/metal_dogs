package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const reconcilePageSize = 100

// ReconcileLookback is how far ListCreatedSince reaches on an unscoped run.
// Older orders stay out of the cron and the unscoped CLI report; --order still inspects them.
const ReconcileLookback = 30 * 24 * time.Hour

// ReconcileReasonMissing is the report reason when a design line has no snapshot and no in-flight or failed job.
const ReconcileReasonMissing = "missing_snapshot"

// ReconcileReasonPending is the report reason when the snapshot job is still pending, processing, or reclaiming.
const ReconcileReasonPending = "snapshot_job_pending"

// ReconcileReasonInspectFailed is the report reason when one order could not be read. The sweep continues.
const ReconcileReasonInspectFailed = "inspect_failed"

// ReconcileGap is one design line that has no snapshot.
type ReconcileGap struct {
	OrderID   string
	VariantID string
	LineKey   string
	Reason    string
}

// LineKey is the snapshot identity (order_id, variant_id) as order_id/variant_id.
func LineKey(orderID, variantID string) string {
	return orderID + "/" + variantID
}

// ReconcileSnapshots lists design lines that have no snapshot.
// Fill, when set, records ready production artwork on snapshots that still have none.
// It does not insert snapshots.
type ReconcileSnapshots struct {
	Orders    ports.OrderSource
	Snapshots ports.OrderDesignSnapshotRepository
	Versions  ports.DesignVersionRepository
	// JobReport classifies the snapshot job for the order.
	// pending is set while the job is in flight. failed is set when it is terminal-failed.
	// Both false means there is no job, or it is done or cancelled. Nil skips the lookup.
	JobReport func(ctx context.Context, orderID string) (pending bool, failed bool, reason string, err error)
	// Clock bounds the unscoped list. Nil uses time.Now.
	Clock ports.Clock
	Fill  bool
}

// Run reports design lines with no snapshot.
// An empty orderID lists orders created within ReconcileLookback.
// A canonical orderID inspects that order and does not call ListCreatedSince.
func (r *ReconcileSnapshots) Run(ctx context.Context, orderID string) ([]ReconcileGap, error) {
	if r == nil || r.Orders == nil || r.Snapshots == nil {
		return nil, fmt.Errorf("metaldogs reconcile: dependencies required")
	}
	only, err := canonicalReconcileOrder(orderID)
	if err != nil {
		return nil, err
	}
	if only != "" {
		return r.finishOrder(ctx, only)
	}
	var gaps []ReconcileGap
	var failed error
	var after string
	since := r.now().Add(-ReconcileLookback)
	for {
		ids, err := r.Orders.ListCreatedSince(ctx, since, after, reconcilePageSize)
		if err != nil {
			return gaps, errors.Join(failed, fmt.Errorf("metaldogs reconcile: list orders: %w", err))
		}
		if len(ids) == 0 {
			break
		}
		found, err := r.pageGaps(ctx, ids)
		gaps = append(gaps, found...)
		if err != nil {
			failed = errors.Join(failed, err)
		}
		after = ids[len(ids)-1]
		if len(ids) < reconcilePageSize {
			break
		}
	}
	return gaps, failed
}

func (r *ReconcileSnapshots) now() time.Time {
	if r == nil || r.Clock == nil {
		return time.Now().UTC()
	}
	return r.Clock.Now().UTC()
}

func (r *ReconcileSnapshots) pageGaps(ctx context.Context, ids []string) ([]ReconcileGap, error) {
	batch, err := r.Orders.LinesForOrders(ctx, ids)
	if err != nil {
		// The batch error names no order. Read each id so one failure stays inspect_failed.
		return r.pageGapsByLine(ctx, ids)
	}
	var gaps []ReconcileGap
	var failed error
	for _, id := range ids {
		found, err := r.finishLines(ctx, id, batch[id])
		gaps = append(gaps, found...)
		if err != nil {
			failed = errors.Join(failed, err)
		}
	}
	return gaps, failed
}

func (r *ReconcileSnapshots) pageGapsByLine(ctx context.Context, ids []string) ([]ReconcileGap, error) {
	var gaps []ReconcileGap
	var failed error
	for _, id := range ids {
		found, err := r.finishOrder(ctx, id)
		gaps = append(gaps, found...)
		if err != nil {
			failed = errors.Join(failed, err)
		}
	}
	return gaps, failed
}

func (r *ReconcileSnapshots) finishLines(ctx context.Context, orderID string, lines []ports.OrderLine) ([]ReconcileGap, error) {
	found, err := r.inspectLines(ctx, orderID, lines)
	if err == nil {
		return found, nil
	}
	found = append(found, ReconcileGap{
		OrderID: orderID,
		LineKey: LineKey(orderID, ""),
		Reason:  ReconcileReasonInspectFailed + ": " + oneLine(err.Error()),
	})
	return found, err
}

func (r *ReconcileSnapshots) finishOrder(ctx context.Context, orderID string) ([]ReconcileGap, error) {
	found, err := r.inspect(ctx, orderID)
	if err == nil {
		return found, nil
	}
	found = append(found, ReconcileGap{
		OrderID: orderID,
		LineKey: LineKey(orderID, ""),
		Reason:  ReconcileReasonInspectFailed + ": " + oneLine(err.Error()),
	})
	return found, err
}

func canonicalReconcileOrder(orderID string) (string, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return "", nil
	}
	parsed, err := uuid.Parse(orderID)
	if err != nil || parsed.String() != strings.ToLower(orderID) {
		return "", ports.ErrInvalidOrderID
	}
	return parsed.String(), nil
}

func (r *ReconcileSnapshots) inspect(ctx context.Context, orderID string) ([]ReconcileGap, error) {
	lines, err := r.Orders.Lines(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("metaldogs reconcile: lines %s: %w", orderID, err)
	}
	return r.inspectLines(ctx, orderID, lines)
}

func (r *ReconcileSnapshots) inspectLines(ctx context.Context, orderID string, lines []ports.OrderLine) ([]ReconcileGap, error) {
	snaps, err := r.Snapshots.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("metaldogs reconcile: snapshots %s: %w", orderID, err)
	}
	byVariant := make(map[string]domain.OrderDesignSnapshot, len(snaps))
	var versions []string
	seenVersion := map[string]struct{}{}
	for _, snap := range snaps {
		byVariant[strings.TrimSpace(snap.VariantID)] = snap
		if snap.ProductionAssetID != "" || snap.ProductionChecksum != "" {
			continue
		}
		if _, ok := seenVersion[snap.DesignVersionID]; ok {
			continue
		}
		seenVersion[snap.DesignVersionID] = struct{}{}
		versions = append(versions, snap.DesignVersionID)
	}
	var missing []ports.OrderLine
	for _, line := range lines {
		if strings.TrimSpace(line.DesignID) == "" {
			continue
		}
		if _, ok := byVariant[strings.TrimSpace(line.VariantID)]; ok {
			continue
		}
		missing = append(missing, line)
	}
	if len(missing) == 0 {
		if err := r.fillReady(ctx, versions); err != nil {
			return nil, err
		}
		return nil, nil
	}
	reason := ReconcileReasonMissing
	if r.JobReport != nil {
		pending, failed, jobReason, err := r.JobReport(ctx, orderID)
		if err != nil {
			return nil, fmt.Errorf("metaldogs reconcile: snapshot job %s: %w", orderID, err)
		}
		switch {
		case pending:
			reason = ReconcileReasonPending
		case failed:
			jobReason = oneLine(jobReason)
			if jobReason == "" {
				jobReason = "snapshot_job_failed"
			}
			reason = jobReason
		}
	}
	gaps := make([]ReconcileGap, 0, len(missing))
	for _, line := range missing {
		variantID := strings.TrimSpace(line.VariantID)
		gaps = append(gaps, ReconcileGap{
			OrderID:   orderID,
			VariantID: variantID,
			LineKey:   LineKey(orderID, variantID),
			Reason:    reason,
		})
	}
	if err := r.fillReady(ctx, versions); err != nil {
		return gaps, err
	}
	return gaps, nil
}

func (r *ReconcileSnapshots) fillReady(ctx context.Context, versions []string) error {
	if r == nil || !r.Fill || len(versions) == 0 {
		return nil
	}
	if r.Versions == nil {
		return fmt.Errorf("metaldogs reconcile: production fill dependencies required")
	}
	filler := &FillSnapshotProduction{Versions: r.Versions, Snapshots: r.Snapshots}
	for _, id := range versions {
		if err := filler.Execute(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}
