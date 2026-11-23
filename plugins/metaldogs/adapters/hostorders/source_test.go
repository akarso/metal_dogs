package hostorders

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestSource_MapsDesignID(t *testing.T) {
	t.Parallel()
	const (
		orderID   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		missingID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		variant   = "vvvvvvvv-vvvv-4vvv-8vvv-vvvvvvvvvvvv"
	)
	fake := &fakeOrders{
		lines: map[string][]extapi.OrderLineView{
			orderID: {
				{
					VariantID: variant,
					SKU:       "TEE-BLK-M",
					Quantity:  2,
					Extensions: map[string]interface{}{
						ports.DesignIDFieldCode: "  design-1  ",
					},
				},
				{
					VariantID:  "other",
					SKU:        "MUG",
					Quantity:   1,
					Extensions: map[string]interface{}{ports.DesignIDFieldCode: ""},
				},
				{VariantID: "plain", SKU: "HAT", Quantity: 1},
			},
		},
	}
	src := New(fake)
	lines, err := src.Lines(context.Background(), orderID)
	if err != nil || len(lines) != 3 {
		t.Fatalf("lines=%+v err=%v", lines, err)
	}
	if lines[0].VariantID != variant || lines[0].SKU != "TEE-BLK-M" || lines[0].Quantity != 2 || lines[0].DesignID != "design-1" {
		t.Fatalf("mapped %+v", lines[0])
	}
	if lines[1].DesignID != "" || lines[2].DesignID != "" {
		t.Fatalf("empty design id not left unset: %+v", lines)
	}
	missing, err := src.Lines(context.Background(), missingID)
	if err != nil || missing != nil {
		t.Fatalf("missing order lines=%+v err=%v", missing, err)
	}
	batch, err := src.LinesForOrders(context.Background(), []string{orderID, missingID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := batch[missingID]; ok || len(batch[orderID]) != 3 || batch[orderID][0].DesignID != "design-1" {
		t.Fatalf("batch=%+v", batch)
	}
}

func TestSource_NonStringDesignID(t *testing.T) {
	t.Parallel()
	const orderID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, raw := range []interface{}{7, true, 1.5} {
		fake := &fakeOrders{lines: map[string][]extapi.OrderLineView{
			orderID: {{
				VariantID:  "v",
				Extensions: map[string]interface{}{ports.DesignIDFieldCode: raw},
			}},
		}}
		_, err := New(fake).Lines(context.Background(), orderID)
		if err == nil || !strings.Contains(err.Error(), ports.DesignIDFieldCode) {
			t.Fatalf("raw %T err=%v", raw, err)
		}
		if _, err := New(fake).LinesForOrders(context.Background(), []string{orderID}); err == nil {
			t.Fatalf("batch accepted %T", raw)
		}
	}
}

func TestSource_ListCreatedSincePassesOrder(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	fake := &fakeOrders{listed: []extapi.OrderView{{ID: "b"}, {ID: "a"}}}
	ids, err := New(fake).ListCreatedSince(context.Background(), since, "cursor", 2)
	if err != nil || len(ids) != 2 || ids[0] != "b" || ids[1] != "a" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if !fake.since.Equal(since) || fake.afterID != "cursor" || fake.limit != 2 {
		t.Fatalf("call since=%s after=%s limit=%d", fake.since, fake.afterID, fake.limit)
	}
	if _, err := New(fake).ListCreatedSince(context.Background(), since, "", 0); err == nil || fake.calls != 1 {
		t.Fatalf("limit 0 err=%v calls=%d", err, fake.calls)
	}
	if _, err := New(fake).ListCreatedSince(context.Background(), since, "", 101); err == nil || fake.calls != 1 {
		t.Fatalf("limit 101 err=%v calls=%d", err, fake.calls)
	}
}

func TestSource_ExistsUsesFindByID(t *testing.T) {
	t.Parallel()
	const id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	fake := &findOrders{hit: map[string]*extapi.OrderView{id: {ID: id}}}
	lookup, ok := New(fake).(ports.OrderLookup)
	if !ok || lookup == nil {
		t.Fatal("source does not implement OrderLookup")
	}
	got, err := lookup.Exists(context.Background(), id)
	if err != nil || !got || fake.calls != 1 {
		t.Fatalf("exists=%v err=%v calls=%d", got, err, fake.calls)
	}
	got, err = lookup.Exists(context.Background(), "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil || got || fake.calls != 2 {
		t.Fatalf("missing exists=%v err=%v calls=%d", got, err, fake.calls)
	}
	fake.fail = errors.New("orders down")
	if _, err := lookup.Exists(context.Background(), id); err == nil || fake.calls != 3 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
}

func TestFromHost_AbsentIsNil(t *testing.T) {
	t.Parallel()
	if FromHost(nil) != nil {
		t.Fatal("nil host")
	}
	if FromHost(orderHost{ok: false}) != nil {
		t.Fatal("unbound port")
	}
	if FromHost(orderHost{ok: true}) != nil {
		t.Fatal("nil port")
	}
	if New(nil) != nil {
		t.Fatal("nil port constructor")
	}
	bound := FromHost(orderHost{ok: true, port: &fakeOrders{}})
	if bound == nil {
		t.Fatal("bound port")
	}
}

type orderHost struct {
	port pluginsdk.OrderPort
	ok   bool
}

func (h orderHost) Orders() (pluginsdk.OrderPort, bool) {
	if !h.ok {
		return nil, false
	}
	return h.port, true
}

type fakeOrders struct {
	lines   map[string][]extapi.OrderLineView
	listed  []extapi.OrderView
	since   time.Time
	afterID string
	limit   int
	calls   int
}

func (f *fakeOrders) FindByID(context.Context, string) (*extapi.OrderView, error) {
	return nil, nil
}

func (f *fakeOrders) Lines(_ context.Context, orderID string) ([]extapi.OrderLineView, error) {
	lines, ok := f.lines[orderID]
	if !ok {
		return nil, nil
	}
	return lines, nil
}

func (f *fakeOrders) LinesForOrders(_ context.Context, orderIDs []string) (map[string][]extapi.OrderLineView, error) {
	out := map[string][]extapi.OrderLineView{}
	for _, id := range orderIDs {
		lines, ok := f.lines[id]
		if !ok {
			continue
		}
		out[id] = lines
	}
	return out, nil
}

type findOrders struct {
	fakeOrders
	hit   map[string]*extapi.OrderView
	calls int
	fail  error
}

func (f *findOrders) FindByID(_ context.Context, id string) (*extapi.OrderView, error) {
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	return f.hit[id], nil
}

func (f *fakeOrders) ListCreatedSince(_ context.Context, since time.Time, afterID string, limit int) ([]extapi.OrderView, error) {
	f.calls++
	f.since = since
	f.afterID = afterID
	f.limit = limit
	return f.listed, nil
}
