package hostorders

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Source reads orders through Host.Orders().
type Source struct {
	orders pluginsdk.OrderPort
}

var _ ports.OrderSource = (*Source)(nil)

// New wraps a bound orders port. A nil port returns a nil source.
func New(orders pluginsdk.OrderPort) ports.OrderSource {
	if orders == nil {
		return nil
	}
	return &Source{orders: orders}
}

// FromHost binds Host.Orders() when that port is present.
// An absent port returns nil so the snapshot job stays fail-closed.
func FromHost(h interface {
	Orders() (pluginsdk.OrderPort, bool)
}) ports.OrderSource {
	if h == nil {
		return nil
	}
	orders, ok := h.Orders()
	if !ok || orders == nil {
		return nil
	}
	return New(orders)
}

// Lines maps one order. An order the port does not return has no lines.
func (s *Source) Lines(ctx context.Context, orderID string) ([]ports.OrderLine, error) {
	if s == nil || s.orders == nil {
		return nil, fmt.Errorf("metaldogs orders: port is not bound")
	}
	raw, err := s.orders.Lines(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	return mapLines(orderID, raw)
}

// LinesForOrders maps each order the port returns. Missing ids are omitted.
func (s *Source) LinesForOrders(ctx context.Context, orderIDs []string) (map[string][]ports.OrderLine, error) {
	if s == nil || s.orders == nil {
		return nil, fmt.Errorf("metaldogs orders: port is not bound")
	}
	raw, err := s.orders.LinesForOrders(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]ports.OrderLine, len(raw))
	for id, lines := range raw {
		mapped, err := mapLines(id, lines)
		if err != nil {
			return nil, err
		}
		out[id] = mapped
	}
	return out, nil
}

// ListCreatedSince returns ids in the port's (created_at, id) order.
// limit must be 1..100. The port rejects an afterID outside the since window.
func (s *Source) ListCreatedSince(ctx context.Context, since time.Time, afterID string, limit int) ([]string, error) {
	if s == nil || s.orders == nil {
		return nil, fmt.Errorf("metaldogs orders: port is not bound")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("metaldogs orders: list created since: limit must be 1..100")
	}
	views, err := s.orders.ListCreatedSince(ctx, since, afterID, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(views))
	for i := range views {
		ids = append(ids, views[i].ID)
	}
	return ids, nil
}

func mapLines(orderID string, lines []extapi.OrderLineView) ([]ports.OrderLine, error) {
	out := make([]ports.OrderLine, len(lines))
	for i, line := range lines {
		designID, err := designID(orderID, line)
		if err != nil {
			return nil, err
		}
		out[i] = ports.OrderLine{
			VariantID: line.VariantID,
			SKU:       line.SKU,
			Quantity:  line.Quantity,
			DesignID:  designID,
		}
	}
	return out, nil
}

func designID(orderID string, line extapi.OrderLineView) (string, error) {
	if line.Extensions == nil {
		return "", nil
	}
	raw, ok := line.Extensions[ports.DesignIDFieldCode]
	if !ok || raw == nil {
		return "", nil
	}
	id, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("metaldogs orders: %s on order %s variant %s is %T", ports.DesignIDFieldCode, orderID, line.VariantID, raw)
	}
	return strings.TrimSpace(id), nil
}
