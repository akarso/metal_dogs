package application

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestReconcileRun_WindowDirectInspectAndPending(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	recent := uuid.NewString()
	recentVariant := uuid.NewString()
	old := uuid.NewString()
	oldVariant := uuid.NewString()
	held := uuid.NewString()
	heldVariant := uuid.NewString()
	pending := uuid.NewString()
	pendingVariant := uuid.NewString()
	bad := uuid.NewString()
	orders := &reconcileOrderStub{
		lines: map[string][]ports.OrderLine{
			recent:  {{VariantID: recentVariant, SKU: "TEE-M", Quantity: 1, DesignID: "des-recent"}},
			old:     {{VariantID: oldVariant, SKU: "TEE-M", Quantity: 1, DesignID: "des-old"}},
			held:    {{VariantID: heldVariant, SKU: "TEE-L", Quantity: 1, DesignID: "des-held"}},
			pending: {{VariantID: pendingVariant, SKU: "TEE-S", Quantity: 1, DesignID: "des-pending"}},
			bad:     {{VariantID: uuid.NewString(), SKU: "TEE-M", Quantity: 1, DesignID: "des-bad"}},
		},
		when: map[string]time.Time{
			recent:  now.Add(-time.Hour),
			old:     now.Add(-ReconcileLookback - time.Hour),
			held:    now.Add(-2 * time.Hour),
			pending: now.Add(-3 * time.Hour),
			bad:     now.Add(-4 * time.Hour),
		},
		lineErr: map[string]error{bad: errors.New("lines down")},
	}
	snaps := reconcileSnapStub{byOrder: map[string][]domain.OrderDesignSnapshot{
		held: {{OrderID: held, VariantID: heldVariant}},
	}}
	svc := &ReconcileSnapshots{
		Orders:    orders,
		Snapshots: snaps,
		Clock:     reconcileClock(now),
		JobReport: func(_ context.Context, orderID string) (bool, bool, string, error) {
			if orderID == pending {
				return true, false, "", nil
			}
			return false, false, "", nil
		},
	}
	gaps, err := svc.Run(context.Background(), "")
	if err == nil || !errors.Is(err, orders.lineErr[bad]) {
		t.Fatalf("err=%v", err)
	}
	if orders.lists == 0 {
		t.Fatal("unscoped run did not list")
	}
	got := map[string]string{}
	for _, gap := range gaps {
		got[gap.OrderID] = gap.Reason
	}
	if got[recent] != ReconcileReasonMissing || got[pending] != ReconcileReasonPending {
		t.Fatalf("%v", got)
	}
	if _, ok := got[old]; ok {
		t.Fatalf("old order reported %v", got)
	}
	if _, ok := got[held]; ok {
		t.Fatalf("snapshotted order reported %v", got)
	}
	if got[bad] == "" || len(got[bad]) < len(ReconcileReasonInspectFailed) || got[bad][:len(ReconcileReasonInspectFailed)] != ReconcileReasonInspectFailed {
		t.Fatalf("bad %q", got[bad])
	}

	orders.lists = 0
	one, err := svc.Run(context.Background(), old)
	if err != nil || len(one) != 1 || one[0].LineKey != old+"/"+oldVariant || one[0].Reason != ReconcileReasonMissing {
		t.Fatalf("err=%v gaps=%+v", err, one)
	}
	if orders.lists != 0 {
		t.Fatalf("scoped run listed %d", orders.lists)
	}
}

func TestReconcileRun_PagesEachOrderOnce(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	const n = reconcilePageSize + 1
	ids := make([]string, n)
	lines := make(map[string][]ports.OrderLine, n)
	for i := range ids {
		ids[i] = uuid.NewString()
		lines[ids[i]] = []ports.OrderLine{{
			VariantID: uuid.NewString(), SKU: "TEE-M", Quantity: 1, DesignID: "des-page",
		}}
	}
	sort.Strings(ids)
	orders := &pageOrderStub{ids: ids, when: now.Add(-time.Hour), lines: lines}
	svc := &ReconcileSnapshots{
		Orders:    orders,
		Snapshots: reconcileSnapStub{},
		Clock:     reconcileClock(now),
	}
	gaps, err := svc.Run(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, gap := range gaps {
		seen[gap.OrderID]++
		if gap.Reason != ReconcileReasonMissing {
			t.Fatalf("%s %s", gap.OrderID, gap.Reason)
		}
	}
	if len(seen) != n {
		t.Fatalf("reported %d", len(seen))
	}
	for _, id := range ids {
		if seen[id] != 1 {
			t.Fatalf("%s count %d", id, seen[id])
		}
	}
	if len(orders.lists) != 2 || len(orders.batches) != 2 {
		t.Fatalf("lists=%d batches=%d", len(orders.lists), len(orders.batches))
	}
	if !orders.lists[0].since.Equal(orders.lists[1].since) || orders.lists[0].after != "" || orders.lists[1].after != ids[reconcilePageSize-1] {
		t.Fatalf("cursors %+v want after %s", orders.lists, ids[reconcilePageSize-1])
	}
	if orders.lists[0].limit != reconcilePageSize || orders.lists[1].limit != reconcilePageSize {
		t.Fatalf("limits %+v", orders.lists)
	}
	if len(orders.batches[0]) != reconcilePageSize || len(orders.batches[1]) != 1 || orders.lineCalls != 0 {
		t.Fatalf("batch sizes %d %d lines %d", len(orders.batches[0]), len(orders.batches[1]), orders.lineCalls)
	}

	orders.lists = nil
	orders.batches = nil
	one, err := svc.Run(context.Background(), ids[0])
	if err != nil || len(one) != 1 || one[0].OrderID != ids[0] {
		t.Fatalf("err=%v gaps=%+v", err, one)
	}
	if len(orders.lists) != 0 || len(orders.batches) != 0 || orders.lineCalls != 1 {
		t.Fatalf("scoped lists=%d batches=%d lines=%d", len(orders.lists), len(orders.batches), orders.lineCalls)
	}
}

func TestReconcileRun_PageBatchContextSkipsLineFallback(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	ids := make([]string, reconcilePageSize)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	sort.Strings(ids)
	orders := &pageOrderStub{
		ids:      ids,
		when:     now.Add(-time.Hour),
		batchErr: context.Canceled,
	}
	svc := &ReconcileSnapshots{
		Orders:    orders,
		Snapshots: reconcileSnapStub{},
		Clock:     reconcileClock(now),
	}
	gaps, err := svc.Run(context.Background(), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(gaps) != 0 || orders.lineCalls != 0 || len(orders.lists) != 1 || len(orders.batches) != 1 {
		t.Fatalf("gaps=%d lines=%d lists=%d batches=%d", len(gaps), orders.lineCalls, len(orders.lists), len(orders.batches))
	}

	orders.lists = nil
	orders.batches = nil
	orders.batchErr = context.DeadlineExceeded
	gaps, err = svc.Run(context.Background(), "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if len(gaps) != 0 || orders.lineCalls != 0 || len(orders.lists) != 1 {
		t.Fatalf("gaps=%d lines=%d lists=%d", len(gaps), orders.lineCalls, len(orders.lists))
	}

	orders.lists = nil
	orders.batches = nil
	orders.batchErr = errors.New("batch unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gaps, err = svc.Run(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(gaps) != 0 || orders.lineCalls != 0 || len(orders.lists) != 1 {
		t.Fatalf("gaps=%d lines=%d lists=%d", len(gaps), orders.lineCalls, len(orders.lists))
	}
}

func TestReconcileRun_PageBatchErrorFallsBackPerOrder(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	id := uuid.NewString()
	orders := &pageOrderStub{
		ids:      []string{id},
		when:     now.Add(-time.Hour),
		batchErr: errors.New("batch unavailable"),
		lines: map[string][]ports.OrderLine{
			id: {{VariantID: uuid.NewString(), SKU: "TEE-M", Quantity: 1, DesignID: "des-1"}},
		},
	}
	svc := &ReconcileSnapshots{
		Orders:    orders,
		Snapshots: reconcileSnapStub{},
		Clock:     reconcileClock(now),
	}
	gaps, err := svc.Run(context.Background(), "")
	if err != nil || len(gaps) != 1 || gaps[0].OrderID != id || gaps[0].Reason != ReconcileReasonMissing {
		t.Fatalf("err=%v gaps=%+v", err, gaps)
	}
	if orders.lineCalls != 1 || len(orders.batches) != 1 {
		t.Fatalf("lines=%d batches=%d", orders.lineCalls, len(orders.batches))
	}
}

type pageListCall struct {
	since time.Time
	after string
	limit int
}

type pageOrderStub struct {
	ids       []string
	when      time.Time
	lines     map[string][]ports.OrderLine
	lists     []pageListCall
	batches   [][]string
	lineCalls int
	batchErr  error
}

func (o *pageOrderStub) Lines(_ context.Context, orderID string) ([]ports.OrderLine, error) {
	o.lineCalls++
	return o.lines[orderID], nil
}

func (o *pageOrderStub) LinesForOrders(_ context.Context, orderIDs []string) (map[string][]ports.OrderLine, error) {
	o.batches = append(o.batches, append([]string(nil), orderIDs...))
	if o.batchErr != nil {
		return nil, o.batchErr
	}
	out := make(map[string][]ports.OrderLine, len(orderIDs))
	for _, id := range orderIDs {
		if lines, ok := o.lines[id]; ok {
			out[id] = lines
		}
	}
	return out, nil
}

func (o *pageOrderStub) ListCreatedSince(_ context.Context, since time.Time, afterID string, limit int) ([]string, error) {
	o.lists = append(o.lists, pageListCall{since: since, after: afterID, limit: limit})
	var out []string
	for _, id := range o.ids {
		if o.when.Before(since) {
			continue
		}
		if afterID != "" && id <= afterID {
			continue
		}
		out = append(out, id)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

type reconcileOrderStub struct {
	lines   map[string][]ports.OrderLine
	when    map[string]time.Time
	lineErr map[string]error
	lists   int
}

func (o *reconcileOrderStub) Lines(_ context.Context, orderID string) ([]ports.OrderLine, error) {
	if err := o.lineErr[orderID]; err != nil {
		return nil, err
	}
	return o.lines[orderID], nil
}

func (o *reconcileOrderStub) LinesForOrders(ctx context.Context, orderIDs []string) (map[string][]ports.OrderLine, error) {
	out := map[string][]ports.OrderLine{}
	for _, id := range orderIDs {
		if _, known := o.lines[id]; !known {
			if _, failed := o.lineErr[id]; !failed {
				continue
			}
		}
		lines, err := o.Lines(ctx, id)
		if err != nil {
			return nil, err
		}
		out[id] = lines
	}
	return out, nil
}

func (o *reconcileOrderStub) ListCreatedSince(_ context.Context, since time.Time, afterID string, limit int) ([]string, error) {
	o.lists++
	if limit <= 0 {
		return nil, nil
	}
	var ids []string
	for id, at := range o.when {
		if at.Before(since) {
			continue
		}
		if afterID != "" && id <= afterID {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

type reconcileSnapStub struct {
	byOrder map[string][]domain.OrderDesignSnapshot
}

func (reconcileSnapStub) Insert(context.Context, domain.OrderDesignSnapshot) (bool, error) {
	return false, nil
}
func (reconcileSnapStub) CommitOrder(context.Context, []domain.OrderDesignSnapshot, []domain.Asset, time.Time) ([]bool, []bool, error) {
	return nil, nil, nil
}
func (reconcileSnapStub) Get(context.Context, string, string) (domain.OrderDesignSnapshot, bool, error) {
	return domain.OrderDesignSnapshot{}, false, nil
}
func (s reconcileSnapStub) ListByOrder(_ context.Context, orderID string) ([]domain.OrderDesignSnapshot, error) {
	return s.byOrder[orderID], nil
}
func (reconcileSnapStub) ListUnfilledProduction(context.Context, string) ([]domain.OrderDesignSnapshot, error) {
	return nil, nil
}
func (reconcileSnapStub) FillProduction(context.Context, string, string, string, string) error {
	return nil
}

type reconcileClock time.Time

func (c reconcileClock) Now() time.Time { return time.Time(c) }
