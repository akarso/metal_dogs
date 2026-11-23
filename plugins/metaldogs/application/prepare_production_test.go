package application_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

func TestProductionAssetID_IsV5(t *testing.T) {
	t.Parallel()
	const versionID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	got, err := application.ProductionAssetID("  " + strings.ToUpper(versionID) + " ")
	if err != nil {
		t.Fatal(err)
	}
	again, err := application.ProductionAssetID(versionID)
	if err != nil || again != got {
		t.Fatalf("again=%s err=%v", again, err)
	}
	parsed, err := uuid.Parse(got)
	if err != nil || parsed.Version() != 5 {
		t.Fatalf("id %s err %v", got, err)
	}
	other, err := application.ProductionAssetID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil || other == got {
		t.Fatalf("other %s err %v", other, err)
	}
	if _, err := application.ProductionAssetID("not-a-uuid"); err == nil {
		t.Fatal("non-uuid version id must fail")
	}
}
