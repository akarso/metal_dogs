package application

import (
	"context"
	"errors"
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
func (reconcileSnapStub) CommitOrder(context.Context, []domain.OrderDesignSnapshot, time.Time) ([]bool, error) {
	return nil, nil
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
