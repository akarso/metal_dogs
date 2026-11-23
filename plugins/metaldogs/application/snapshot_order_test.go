package application

import (
	"errors"
	"fmt"
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestCommitOrderError_NamesFailingDesign(t *testing.T) {
	t.Parallel()
	orderID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	snaps := []domain.OrderDesignSnapshot{
		{VariantID: "v-first", DesignID: "d-first"},
		{VariantID: "v-second", DesignID: "d-second"},
	}
	err := commitOrderError(orderID, snaps, &ports.SnapshotDesignError{
		DesignID: "d-second",
		Err:      fmt.Errorf("metaldogs snapshot: design d-second status changed"),
	})
	if err.DesignID != "d-second" || err.VariantID != "v-second" || err.OrderID != orderID {
		t.Fatalf("%+v", err)
	}
	if len(err.FailedLines) != 2 || err.FailedLines[0].DesignID != "d-first" || err.FailedLines[1].VariantID != "v-second" {
		t.Fatalf("rolled back lines %+v", err.FailedLines)
	}

	same := []domain.OrderDesignSnapshot{
		{VariantID: "v-m", DesignID: "d-same"},
		{VariantID: "v-l", DesignID: "d-same"},
	}
	err = commitOrderError(orderID, same, &ports.SnapshotDesignError{
		DesignID: "d-same",
		Err:      fmt.Errorf("metaldogs snapshot: design d-same status changed"),
	})
	if err.DesignID != "d-same" || err.VariantID != "" {
		t.Fatalf("shared design %+v", err)
	}
	if len(err.FailedLines) != 2 || err.FailedLines[0].VariantID != "v-m" || err.FailedLines[1].VariantID != "v-l" {
		t.Fatalf("shared lines %+v", err.FailedLines)
	}

	err = commitOrderError(orderID, snaps, fmt.Errorf("metaldogs order_design_snapshots begin: down"))
	if err.DesignID != "" || err.VariantID != "" || err.OrderID != orderID || len(err.FailedLines) != 2 {
		t.Fatalf("unknown line %+v", err)
	}
	if !errors.Is(err, err.Err) {
		t.Fatal("cause dropped")
	}
}

func TestSnapshotOrder_RequiresClock(t *testing.T) {
	t.Parallel()
	_, err := (&SnapshotOrder{}).Execute(t.Context(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err == nil || err.Error() != "metaldogs snapshot: dependencies required" {
		t.Fatalf("%v", err)
	}
}
