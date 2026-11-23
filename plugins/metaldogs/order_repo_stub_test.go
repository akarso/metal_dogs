package metaldogs_test

import (
	"context"
	"time"

	domainorder "github.com/akarso/shopanda/internal/domain/order"
)

// idleOrderRepo is a bound OrderRepository with no rows, enough for Host.Orders().
type idleOrderRepo struct{}

func (idleOrderRepo) FindByID(context.Context, string) (*domainorder.Order, error) {
	return nil, nil
}
func (idleOrderRepo) FindByIDs(context.Context, []string) (map[string]*domainorder.Order, error) {
	return map[string]*domainorder.Order{}, nil
}
func (idleOrderRepo) FindByCustomerID(context.Context, string) ([]domainorder.Order, error) {
	return nil, nil
}
func (idleOrderRepo) FindByContactEmail(context.Context, string) ([]domainorder.Order, error) {
	return nil, nil
}
func (idleOrderRepo) List(context.Context, int, int) ([]domainorder.Order, error) { return nil, nil }
func (idleOrderRepo) ListCreatedSince(context.Context, time.Time, string, int) ([]domainorder.Order, error) {
	return nil, nil
}
func (idleOrderRepo) Save(context.Context, *domainorder.Order) error         { return nil }
func (idleOrderRepo) UpdateStatus(context.Context, *domainorder.Order) error { return nil }
func (idleOrderRepo) LinkToCustomer(context.Context, *domainorder.Order) error {
	return nil
}
func (idleOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (idleOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]domainorder.TaxSnapshotRow, error) {
	return nil, nil
}
