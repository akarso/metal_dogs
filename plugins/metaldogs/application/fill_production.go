package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// FillSnapshotProduction copies ready production artwork onto snapshots that still have none.
// A version that is not ready, or a second call after the columns are set, changes nothing.
type FillSnapshotProduction struct {
	Versions  ports.DesignVersionRepository
	Snapshots ports.OrderDesignSnapshotRepository
}

// Execute sets production fields once for every unfilled snapshot of versionID.
func (f *FillSnapshotProduction) Execute(ctx context.Context, versionID string) error {
	if f == nil || f.Versions == nil || f.Snapshots == nil {
		return fmt.Errorf("metaldogs snapshot: production fill dependencies required")
	}
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return nil
	}
	v, ok, err := f.Versions.GetByID(ctx, versionID)
	if err != nil {
		return fmt.Errorf("metaldogs snapshot: production fill version %s: %w", versionID, err)
	}
	if !ok || v.ProductionStatus != domain.ProductionReady {
		return nil
	}
	if strings.TrimSpace(v.ProductionAssetID) == "" || strings.TrimSpace(v.ProductionChecksum) == "" {
		return nil
	}
	rows, err := f.Snapshots.ListUnfilledProduction(ctx, versionID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		err := f.Snapshots.FillProduction(ctx, row.OrderID, row.VariantID, v.ProductionAssetID, v.ProductionChecksum)
		if err == nil || errors.Is(err, domain.ErrProductionAlreadySet) || errors.Is(err, ports.ErrSnapshotNotFound) {
			continue
		}
		return err
	}
	return nil
}
