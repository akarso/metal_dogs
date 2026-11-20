package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Health returns GET /api/customizer/health — coarse plugin/queue readiness.
// Probes a bound queue that can read job rows (no per-type counts).
// Depths and typed failure reasons stay on AdminHealth (jobs.read).
// Not a k8s probe (use /healthz and /readyz).
//
// Degraded responses use HTTP 503 with a generic reason so accidental
// LB/uptime wiring fails closed without leaking recon detail.
func Health(jobs ports.JobHealth) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp, code := evaluatePublicHealth(jobs)
		writeHealth(w, code, resp)
	})
}

// AdminHealth returns GET /api/v1/admin/customizer/health — per-type depths
// for registered metaldogs sync jobs. Always includes every registered type;
// query failures keep the row with error set.
func AdminHealth(jobs ports.JobHealth) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp, code := evaluateAdminHealth(r, jobs)
		writeHealth(w, code, resp)
	})
}

func evaluatePublicHealth(jobs ports.JobHealth) (healthResponse, int) {
	resp := healthResponse{Status: "ok", Plugin: "metaldogs"}
	if jobs == nil || !jobs.QueueBound() || !jobs.ReaderBound() {
		resp.Status = "degraded"
		resp.Reason = "degraded"
		return resp, http.StatusServiceUnavailable
	}
	return resp, http.StatusOK
}

func evaluateAdminHealth(r *http.Request, jobs ports.JobHealth) (healthResponse, int) {
	resp := healthResponse{
		Status: "ok",
		Plugin: "metaldogs",
		Queues: []queueDepth{},
	}
	if jobs == nil {
		resp.Status = "degraded"
		resp.Reason = "app_unconfigured"
		return resp, http.StatusServiceUnavailable
	}

	types := jobs.JobTypes("metaldogs")
	if !jobs.QueueBound() {
		resp.Status = "degraded"
		resp.Reason = "queue_unbound"
		for _, jobType := range types {
			resp.Queues = append(resp.Queues, queueDepth{Type: jobType, Error: "queue_unbound"})
		}
		return resp, http.StatusServiceUnavailable
	}
	if !jobs.ReaderBound() {
		resp.Status = "degraded"
		resp.Reason = "reader_unavailable"
		for _, jobType := range types {
			resp.Queues = append(resp.Queues, queueDepth{Type: jobType, Error: "reader_unavailable"})
		}
		return resp, http.StatusServiceUnavailable
	}

	countsFailed := false
	for _, jobType := range types {
		counts, err := jobs.CountsByType(r.Context(), jobType)
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
			Pending:    counts[ports.JobStatusPending],
			Processing: counts[ports.JobStatusProcessing],
			Failed:     counts[ports.JobStatusFailed],
		})
	}
	if countsFailed {
		resp.Status = "degraded"
		resp.Reason = "counts_failed"
		return resp, http.StatusServiceUnavailable
	}
	return resp, http.StatusOK
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
