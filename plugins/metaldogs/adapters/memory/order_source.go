package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	byID, err := m.LinesForOrders(ctx, []string{orderID})
	if err != nil {
		return nil, err
	}
	lines, ok := byID[strings.TrimSpace(orderID)]
	if !ok {
		return nil, nil
	}
	return lines, nil
}

// LinesForOrders returns a copy of the lines for each known id. Missing ids are omitted.
func (m *OrderSource) LinesForOrders(ctx context.Context, orderIDs []string) (map[string][]ports.OrderLine, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	out := map[string][]ports.OrderLine{}
	if m == nil {
		return out, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[string]struct{}, len(orderIDs))
	for _, id := range orderIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		stored, ok := m.orders[id]
		if !ok {
			continue
		}
		out[id] = append([]ports.OrderLine(nil), stored.lines...)
	}
	return out, nil
}

// ListCreatedSince returns order ids created at or after since, in (created_at, id) order.
// afterID must name an order inside that window. limit must be 1..100.
func (m *OrderSource) ListCreatedSince(ctx context.Context, since time.Time, afterID string, limit int) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("metaldogs orders: list created since: source is nil")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("metaldogs orders: list created since: limit must be 1..100")
	}
	since = since.UTC()
	afterID = strings.TrimSpace(afterID)
	m.mu.Lock()
	defer m.mu.Unlock()
	var afterCreated time.Time
	if afterID != "" {
		cursor, ok := m.orders[afterID]
		if !ok {
			return nil, fmt.Errorf("metaldogs orders: list created since: after_id %q not found", afterID)
		}
		afterCreated = cursor.created
		if afterCreated.Before(since) {
			return nil, fmt.Errorf("metaldogs orders: list created since: after_id %q is before since", afterID)
		}
	}
	page := make([]storedOrder, 0, len(m.orders))
	for _, o := range m.orders {
		if o.created.Before(since) {
			continue
		}
		if afterID != "" && (o.created.Before(afterCreated) || (o.created.Equal(afterCreated) && o.id <= afterID)) {
			continue
		}
		page = append(page, o)
	}
	sort.Slice(page, func(i, j int) bool {
		if !page[i].created.Equal(page[j].created) {
			return page[i].created.Before(page[j].created)
		}
		return page[i].id < page[j].id
	})
	if len(page) > limit {
		page = page[:limit]
	}
	ids := make([]string, len(page))
	for i := range page {
		ids[i] = page[i].id
	}
	return ids, nil
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
