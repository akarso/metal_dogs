package observability

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

// CorrelationID returns the platform request_id for ctx (X-Request-ID).
// Metal Dogs runbooks also call this correlation_id — same value.
//
// Sync workers re-inject payload request_id into ctx before handlers run
// (see integration sync_runtime). Prefer JobLogFields when logging from a
// SyncJobContext so correlation is taken from the durable payload.
func CorrelationID(ctx context.Context) string {
	return requestctx.RequestID(ctx)
}

// JobLogFields builds structured log fields for a sync job handler.
// Emits platform request_id and the product alias correlation_id when present.
func JobLogFields(job extapi.SyncJobContext) map[string]interface{} {
	fields := map[string]interface{}{
		"job_id":  job.JobID,
		"attempt": job.Attempts,
		"name":    job.Name,
	}
	if rid, _ := job.Payload["request_id"].(string); rid != "" {
		fields["request_id"] = rid
		fields["correlation_id"] = rid
	}
	return fields
}
