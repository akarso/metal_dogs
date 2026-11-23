package memory

import (
	"context"
	"strings"
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
	if err == nil || none != nil {
		t.Fatalf("limit 0 = %v err=%v", none, err)
	}
	batch, err := src.LinesForOrders(context.Background(), []string{
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	})
	if err != nil || len(batch) != 1 || batch["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"][0].DesignID != "d-b" {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
}

func TestOrderSource_ListCreatedSinceKeyset(t *testing.T) {
	t.Parallel()
	src := NewOrderSource()
	t0 := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	early := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	midA := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	midB := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	late := "00000000-0000-4000-8000-000000000001"
	src.Add(early, t0.Add(-time.Hour), nil)
	src.Add(midB, t0, nil)
	src.Add(midA, t0, nil)
	src.Add(late, t0.Add(time.Minute), nil)

	ids, err := src.ListCreatedSince(context.Background(), t0, "", 10)
	if err != nil || len(ids) != 3 || ids[0] != midA || ids[1] != midB || ids[2] != late {
		t.Fatalf("order=%v err=%v", ids, err)
	}
	page, err := src.ListCreatedSince(context.Background(), t0, midA, 1)
	if err != nil || len(page) != 1 || page[0] != midB {
		t.Fatalf("page=%v err=%v", page, err)
	}
	rest, err := src.ListCreatedSince(context.Background(), t0, page[0], 100)
	if err != nil || len(rest) != 1 || rest[0] != late {
		t.Fatalf("rest=%v err=%v", rest, err)
	}
	if _, err := src.ListCreatedSince(context.Background(), t0, early, 10); err == nil || !strings.Contains(err.Error(), "before since") {
		t.Fatalf("before since: %v", err)
	}
	if _, err := src.ListCreatedSince(context.Background(), t0, "cccccccc-cccc-4ccc-8ccc-cccccccccccc", 10); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown cursor: %v", err)
	}
	if _, err := src.ListCreatedSince(context.Background(), t0, "", 101); err == nil || !strings.Contains(err.Error(), "1..100") {
		t.Fatalf("limit 101: %v", err)
	}
	one, err := src.ListCreatedSince(context.Background(), t0, "", 1)
	if err != nil || len(one) != 1 || one[0] != midA {
		t.Fatalf("limit 1 = %v err=%v", one, err)
	}
}
