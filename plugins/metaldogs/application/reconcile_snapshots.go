package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const reconcilePageSize = 100

// ReconcileReasonMissing is the report reason when a design line has no snapshot and no failed job.
const ReconcileReasonMissing = "missing_snapshot"

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
	// JobFailure reports a terminal-failed snapshot job for the order.
	// failed is false when there is no such job. Nil skips the lookup.
	JobFailure func(ctx context.Context, orderID string) (failed bool, reason string, err error)
	Fill       bool
}

// Run reads orders from ListCreatedSince (created_at >= zero, every page) and Lines.
// orderID, when set, limits the report to that order. It must be a canonical UUID.
func (r *ReconcileSnapshots) Run(ctx context.Context, orderID string) ([]ReconcileGap, error) {
	if r == nil || r.Orders == nil || r.Snapshots == nil {
		return nil, fmt.Errorf("metaldogs reconcile: dependencies required")
	}
	only, err := canonicalReconcileOrder(orderID)
	if err != nil {
		return nil, err
	}
	var gaps []ReconcileGap
	var after string
	for {
		ids, err := r.Orders.ListCreatedSince(ctx, time.Time{}, after, reconcilePageSize)
		if err != nil {
			return nil, fmt.Errorf("metaldogs reconcile: list orders: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if only != "" && id != only {
				continue
			}
			found, err := r.inspect(ctx, id)
			if err != nil {
				return nil, err
			}
			gaps = append(gaps, found...)
		}
		after = ids[len(ids)-1]
		if len(ids) < reconcilePageSize {
			break
		}
	}
	return gaps, nil
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
	if r.Fill && len(versions) > 0 {
		if r.Versions == nil {
			return nil, fmt.Errorf("metaldogs reconcile: production fill dependencies required")
		}
		filler := &FillSnapshotProduction{Versions: r.Versions, Snapshots: r.Snapshots}
		for _, id := range versions {
			if err := filler.Execute(ctx, id); err != nil {
				return nil, err
			}
		}
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
		return nil, nil
	}
	reason := ReconcileReasonMissing
	if r.JobFailure != nil {
		failed, jobReason, err := r.JobFailure(ctx, orderID)
		if err != nil {
			return nil, fmt.Errorf("metaldogs reconcile: snapshot job %s: %w", orderID, err)
		}
		if failed {
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
	return gaps, nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}
