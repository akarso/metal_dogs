package memory

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestOrderSource_LinesAndListCreatedSince(t *testing.T) {
	t.Parallel()
	src := NewOrderSource()
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	src.Add("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", at, []ports.OrderLine{{
		VariantID: "v-b", SKU: "TEE-B", Quantity: 1, DesignID: "d-b",
	}})
	src.Add("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", at.Add(-time.Hour), []ports.OrderLine{{
		VariantID: "v-a", SKU: "TEE-A", Quantity: 2,
	}})
	src.Add("cccccccc-cccc-4ccc-8ccc-cccccccccccc", at.Add(time.Hour), nil)

	lines, err := src.Lines(context.Background(), "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil || len(lines) != 1 || lines[0].DesignID != "d-b" || lines[0].Quantity != 1 {
		t.Fatalf("lines=%+v err=%v", lines, err)
	}
	lines[0].SKU = "mutated"
	again, err := src.Lines(context.Background(), "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil || again[0].SKU != "TEE-B" {
		t.Fatalf("lines alias store: %+v err=%v", again, err)
	}
	missing, err := src.Lines(context.Background(), "dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil || missing != nil {
		t.Fatalf("unknown order lines=%+v err=%v", missing, err)
	}

	ids, err := src.ListCreatedSince(context.Background(), at, "", 10)
	if err != nil || len(ids) != 2 || ids[0] != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" || ids[1] != "cccccccc-cccc-4ccc-8ccc-cccccccccccc" {
		t.Fatalf("since=%v err=%v", ids, err)
	}
	page, err := src.ListCreatedSince(context.Background(), at, ids[0], 10)
	if err != nil || len(page) != 1 || page[0] != ids[1] {
		t.Fatalf("after=%v err=%v", page, err)
	}
	none, err := src.ListCreatedSince(context.Background(), at, "", 0)
	if err != nil || none != nil {
		t.Fatalf("limit 0 = %v err=%v", none, err)
	}
}
