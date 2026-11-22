package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

type storedOrder struct {
	id      string
	created time.Time
	lines   []ports.OrderLine
}

// OrderSource is an in-memory ports.OrderSource for tests.
type OrderSource struct {
	mu     sync.Mutex
	orders map[string]storedOrder
}

// NewOrderSource returns an empty source.
func NewOrderSource() *OrderSource {
	return &OrderSource{orders: map[string]storedOrder{}}
}

var _ ports.OrderSource = (*OrderSource)(nil)

// Add stores lines for orderID, replacing any previous lines.
func (m *OrderSource) Add(orderID string, created time.Time, lines []ports.OrderLine) {
	if m == nil {
		return
	}
	copied := append([]ports.OrderLine(nil), lines...)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.orders == nil {
		m.orders = map[string]storedOrder{}
	}
	m.orders[orderID] = storedOrder{id: orderID, created: created.UTC(), lines: copied}
}

// Lines returns a copy of the lines for orderID. An unknown order has no lines.
func (m *OrderSource) Lines(ctx context.Context, orderID string) ([]ports.OrderLine, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.orders[orderID]
	if !ok {
		return nil, nil
	}
	return append([]ports.OrderLine(nil), stored.lines...), nil
}

// ListCreatedSince returns order ids created at or after since, after afterID.
func (m *OrderSource) ListCreatedSince(ctx context.Context, since time.Time, afterID string, limit int) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil || limit <= 0 {
		return nil, nil
	}
	since = since.UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.orders))
	for _, o := range m.orders {
		if o.created.Before(since) {
			continue
		}
		if afterID != "" && o.id <= afterID {
			continue
		}
		ids = append(ids, o.id)
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
