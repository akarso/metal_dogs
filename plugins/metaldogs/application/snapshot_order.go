package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// SnapshotLineOutcome is one design line after a successful commit.
type SnapshotLineOutcome struct {
	VariantID string
	DesignID  string
	Written   bool
}

// SnapshotOrderError is a failed snapshot for one line. The order is unchanged.
type SnapshotOrderError struct {
	OrderID   string
	VariantID string
	DesignID  string
	Err       error
}

func (e *SnapshotOrderError) Error() string {
	if e == nil || e.Err == nil {
		return "snapshot order failed"
	}
	return e.Err.Error()
}

func (e *SnapshotOrderError) Unwrap() error { return e.Err }

// SnapshotOrder writes one immutable snapshot per design line and marks those designs ordered.
type SnapshotOrder struct {
	Orders    ports.OrderSource
	Designs   ports.DesignRepository
	Versions  ports.DesignVersionRepository
	Templates ports.TemplateStore
	Snapshots ports.OrderDesignSnapshotRepository
	Clock     ports.Clock
}

// Execute loads the order and commits every design line, or writes nothing.
func (s *SnapshotOrder) Execute(ctx context.Context, orderID string) ([]SnapshotLineOutcome, error) {
	if s == nil || s.Orders == nil || s.Designs == nil || s.Versions == nil || s.Templates == nil || s.Snapshots == nil || s.Clock == nil {
		return nil, &SnapshotOrderError{Err: fmt.Errorf("metaldogs snapshot: dependencies required")}
	}
	orderID = strings.TrimSpace(orderID)
	if err := canonicalOrderID(orderID); err != nil {
		return nil, &SnapshotOrderError{OrderID: orderID, Err: err}
	}
	lines, err := s.Orders.Lines(ctx, orderID)
	if err != nil {
		return nil, &SnapshotOrderError{OrderID: orderID, Err: fmt.Errorf("metaldogs snapshot: lines: %w", err)}
	}
	now := s.Clock.Now().UTC()
	var snaps []domain.OrderDesignSnapshot
	for _, line := range lines {
		designID := strings.TrimSpace(line.DesignID)
		if designID == "" {
			continue
		}
		snap, err := s.build(ctx, orderID, line, designID, now)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	if len(snaps) == 0 {
		return nil, nil
	}
	written, err := s.Snapshots.CommitOrder(ctx, snaps, now)
	if err != nil {
		return nil, commitOrderError(orderID, snaps, err)
	}
	out := make([]SnapshotLineOutcome, len(snaps))
	for i, snap := range snaps {
		ok := i < len(written) && written[i]
		out[i] = SnapshotLineOutcome{VariantID: snap.VariantID, DesignID: snap.DesignID, Written: ok}
	}
	return out, nil
}

func commitOrderError(orderID string, snaps []domain.OrderDesignSnapshot, err error) *SnapshotOrderError {
	line := &SnapshotOrderError{OrderID: orderID, Err: err}
	var de *ports.SnapshotDesignError
	if !errors.As(err, &de) || de == nil || de.DesignID == "" {
		return line
	}
	line.DesignID = de.DesignID
	line.VariantID = variantForDesign(snaps, de.DesignID)
	return line
}

func variantForDesign(snaps []domain.OrderDesignSnapshot, designID string) string {
	variant := ""
	for _, s := range snaps {
		if s.DesignID != designID {
			continue
		}
		if variant != "" && variant != s.VariantID {
			return ""
		}
		variant = s.VariantID
	}
	return variant
}

func (s *SnapshotOrder) build(ctx context.Context, orderID string, line ports.OrderLine, designID string, now time.Time) (domain.OrderDesignSnapshot, error) {
	fail := func(err error) (domain.OrderDesignSnapshot, error) {
		return domain.OrderDesignSnapshot{}, &SnapshotOrderError{
			OrderID:   orderID,
			VariantID: strings.TrimSpace(line.VariantID),
			DesignID:  designID,
			Err:       err,
		}
	}
	d, ok, err := s.Designs.GetByID(ctx, designID)
	if err != nil {
		return fail(fmt.Errorf("metaldogs snapshot: design %s: %w", designID, err))
	}
	if !ok {
		return fail(fmt.Errorf("metaldogs snapshot: design %s not found", designID))
	}
	next := d
	if err := next.MarkOrdered(now); err != nil {
		return fail(fmt.Errorf("metaldogs snapshot: design %s: %w", designID, err))
	}
	if strings.TrimSpace(d.ApprovedVersionID) == "" {
		return fail(fmt.Errorf("metaldogs snapshot: design %s has no approved version", designID))
	}
	v, ok, err := s.Versions.GetByID(ctx, d.ApprovedVersionID)
	if err != nil {
		return fail(fmt.Errorf("metaldogs snapshot: version %s: %w", d.ApprovedVersionID, err))
	}
	if !ok {
		return fail(fmt.Errorf("metaldogs snapshot: version %s not found", d.ApprovedVersionID))
	}
	tpl, ok, err := s.Templates.GetByID(ctx, v.TemplateID)
	if err != nil {
		return fail(fmt.Errorf("metaldogs snapshot: template %s: %w", v.TemplateID, err))
	}
	if !ok || tpl.Version != v.TemplateVersion {
		return fail(fmt.Errorf("metaldogs snapshot: template %s version %d not found", v.TemplateID, v.TemplateVersion))
	}
	prodID, prodSum := "", ""
	if v.ProductionStatus == domain.ProductionReady {
		if strings.TrimSpace(v.ProductionAssetID) == "" || strings.TrimSpace(v.ProductionChecksum) == "" {
			return fail(fmt.Errorf("metaldogs snapshot: version %s production ready without artwork", v.ID))
		}
		prodID = v.ProductionAssetID
		prodSum = v.ProductionChecksum
	}
	snap, err := domain.NewOrderDesignSnapshot(
		orderID, line.VariantID, d.ID, v.ID, line.SKU,
		tpl.Slug, tpl.Version, v.Texts,
		v.Provider, v.Model, v.PromptVersion,
		v.ArtworkAssetID, v.ArtworkChecksum,
		prodID, prodSum,
		now,
	)
	if err != nil {
		return fail(err)
	}
	return snap, nil
}

func canonicalOrderID(orderID string) error {
	parsed, err := uuid.Parse(orderID)
	if err != nil || parsed.String() != strings.ToLower(orderID) {
		return ports.ErrInvalidOrderID
	}
	return nil
}
