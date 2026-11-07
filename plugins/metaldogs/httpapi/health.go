package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Health returns GET /api/customizer/health — coarse plugin/queue readiness.
// Probes BoundQueue + jobs.Reader only (no CountsByType / no DB aggregations).
// Depths and typed failure reasons stay on AdminHealth (jobs.read).
// Not a k8s probe (use /healthz and /readyz).
//
// Degraded responses use HTTP 503 with a generic reason so accidental
// LB/uptime wiring fails closed without leaking recon detail.
func Health(app *plugin.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp, code := evaluatePublicHealth(app)
		writeHealth(w, code, resp)
	})
}

// AdminHealth returns GET /api/v1/admin/customizer/health — per-type depths
// for registered metaldogs sync jobs via BoundQueue + CountsByType. Always
// includes every registered type; query failures keep the row with error set.
func AdminHealth(app *plugin.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp, code := evaluateAdminHealth(r, app)
		writeHealth(w, code, resp)
	})
}

func evaluatePublicHealth(app *plugin.App) (healthResponse, int) {
	resp := healthResponse{Status: "ok", Plugin: "metaldogs"}
	if app == nil {
		resp.Status = "degraded"
		resp.Reason = "degraded"
		return resp, http.StatusServiceUnavailable
	}
	q, ok := app.BoundQueue()
	if !ok {
		resp.Status = "degraded"
		resp.Reason = "degraded"
		return resp, http.StatusServiceUnavailable
	}
	if _, ok := q.(jobs.Reader); !ok {
		resp.Status = "degraded"
		resp.Reason = "degraded"
		return resp, http.StatusServiceUnavailable
	}
	return resp, http.StatusOK
}

func evaluateAdminHealth(r *http.Request, app *plugin.App) (healthResponse, int) {
	resp := healthResponse{
		Status: "ok",
		Plugin: "metaldogs",
		Queues: []queueDepth{},
	}
	if app == nil {
		resp.Status = "degraded"
		resp.Reason = "app_unconfigured"
		return resp, http.StatusServiceUnavailable
	}

	types := metaldogsJobTypes(app)
	q, ok := app.BoundQueue()
	if !ok {
		resp.Status = "degraded"
		resp.Reason = "queue_unbound"
		for _, jobType := range types {
			resp.Queues = append(resp.Queues, queueDepth{Type: jobType, Error: "queue_unbound"})
		}
		return resp, http.StatusServiceUnavailable
	}
	reader, ok := q.(jobs.Reader)
	if !ok {
		resp.Status = "degraded"
		resp.Reason = "reader_unavailable"
		for _, jobType := range types {
			resp.Queues = append(resp.Queues, queueDepth{Type: jobType, Error: "reader_unavailable"})
		}
		return resp, http.StatusServiceUnavailable
	}

	countsFailed := false
	for _, jobType := range types {
		counts, err := reader.CountsByType(r.Context(), jobType)
		if err != nil {
			countsFailed = true
			resp.Queues = append(resp.Queues, queueDepth{
				Type:  jobType,
				Error: "counts_failed",
			})
			continue
		}
		resp.Queues = append(resp.Queues, queueDepth{
			Type:       jobType,
			Pending:    counts[jobs.StatusPending],
			Processing: counts[jobs.StatusProcessing],
			Failed:     counts[jobs.StatusFailed],
		})
	}
	if countsFailed {
		resp.Status = "degraded"
		resp.Reason = "counts_failed"
		return resp, http.StatusServiceUnavailable
	}
	return resp, http.StatusOK
}

func metaldogsJobTypes(app *plugin.App) []string {
	var out []string
	for _, reg := range app.SyncJobs() {
		if reg.PluginSlug == "metaldogs" {
			out = append(out, reg.JobType)
		}
	}
	return out
}

type healthResponse struct {
	Status string       `json:"status"`
	Plugin string       `json:"plugin"`
	Reason string       `json:"reason,omitempty"`
	Queues []queueDepth `json:"queues,omitempty"`
}

type queueDepth struct {
	Type       string `json:"type"`
	Pending    int    `json:"pending"`
	Processing int    `json:"processing"`
	Failed     int    `json:"failed"`
	Error      string `json:"error,omitempty"`
}

func writeHealth(w http.ResponseWriter, code int, resp healthResponse) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}
