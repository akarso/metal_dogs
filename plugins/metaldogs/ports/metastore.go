package ports

import "context"

// MetaStore persists key/value rows in metaldogs_meta (F2 harness exercise).
type MetaStore interface {
	Put(ctx context.Context, key, value string) error
	Get(ctx context.Context, key string) (string, bool, error)
}
