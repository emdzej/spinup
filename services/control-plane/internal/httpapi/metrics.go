package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/emdzej/spinup/services/control-plane/internal/promql"
)

type metricsResponse struct {
	Range  string             `json:"range"`
	Step   string             `json:"step"`
	Series map[string]seriesW `json:"series"`
}

type seriesW struct {
	Points []promql.Point `json:"points"`
	Unit   string         `json:"unit,omitempty"`
}

// GET /api/v1/applications/{appId}/metrics?range=15m&step=15s
// Returns aggregated CPU + memory time series for the application's pod(s).
// One SpinApp per Application → filter on core.spinkube.dev/app-name = app.Name.
func (s *Server) getApplicationMetrics(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if s.prom == nil {
		http.Error(w, "metrics disabled (SPINUP_PROMETHEUS_URL not set)", http.StatusServiceUnavailable)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}

	rng, step, err := parseRange(r.URL.Query().Get("range"), r.URL.Query().Get("step"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	end := time.Now()
	start := end.Add(-rng)

	ns := s.functions.Namespace
	joinFilter := fmt.Sprintf(
		`kube_pod_labels{namespace=%q, label_core_spinkube_dev_app_name=%q}`,
		ns, app.Name,
	)
	// 5m rate window handles typical cAdvisor scrape gaps and pod restarts
	// more gracefully than 1m without over-smoothing on a 15m+ view.
	cpuQ := fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{namespace=%q, container!="", container!="POD"}[5m]) * on(namespace, pod) group_left %s)`,
		ns, joinFilter,
	)
	memQ := fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace=%q, container!="", container!="POD"} * on(namespace, pod) group_left %s)`,
		ns, joinFilter,
	)

	cpu, err := s.prom.QueryRange(r.Context(), cpuQ, start, end, step)
	if err != nil {
		s.logger.Warn("promql cpu", "err", err)
	}
	mem, err := s.prom.QueryRange(r.Context(), memQ, start, end, step)
	if err != nil {
		s.logger.Warn("promql mem", "err", err)
	}

	writeJSON(w, http.StatusOK, metricsResponse{
		Range: rng.String(),
		Step:  step.String(),
		Series: map[string]seriesW{
			"cpu":    {Points: cpu, Unit: "cores"},
			"memory": {Points: mem, Unit: "bytes"},
		},
	})
}

// GET /api/v1/applications/{appId}/functions/{fnId}/metrics?range=15m&step=15s
// Returns request-rate and p95 latency for a single function, derived from
// OTel spanmetrics (traces_span_metrics_calls_total, ..._duration_milliseconds_bucket).
// Keyed by http_route — each function has a unique route in Spinup.
func (s *Server) getFunctionMetrics(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if s.prom == nil {
		http.Error(w, "metrics disabled (SPINUP_PROMETHEUS_URL not set)", http.StatusServiceUnavailable)
		return
	}
	_, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	rng, step, err := parseRange(r.URL.Query().Get("range"), r.URL.Query().Get("step"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	end := time.Now()
	start := end.Add(-rng)

	// Filter to server-kind spans (one per HTTP request; the internal
	// component-execution span would double-count). The rate window is
	// deliberately shorter than the CPU/memory panel because span-derived
	// metrics arrive as counters at the collector's flush interval.
	route := fn.Route
	reqQ := fmt.Sprintf(
		`sum(rate(traces_span_metrics_calls_total{span_kind="SPAN_KIND_SERVER",http_route=%q}[2m]))`,
		route,
	)
	p95Q := fmt.Sprintf(
		`histogram_quantile(0.95, sum by (le) (rate(traces_span_metrics_duration_milliseconds_bucket{span_kind="SPAN_KIND_SERVER",http_route=%q}[2m])))`,
		route,
	)
	errQ := fmt.Sprintf(
		`sum(rate(traces_span_metrics_calls_total{span_kind="SPAN_KIND_SERVER",http_route=%q,http_response_status_code=~"5.."}[2m]))`,
		route,
	)

	reqs, err := s.prom.QueryRange(r.Context(), reqQ, start, end, step)
	if err != nil {
		s.logger.Warn("promql fn req rate", "err", err)
	}
	p95, err := s.prom.QueryRange(r.Context(), p95Q, start, end, step)
	if err != nil {
		s.logger.Warn("promql fn p95", "err", err)
	}
	errs, err := s.prom.QueryRange(r.Context(), errQ, start, end, step)
	if err != nil {
		s.logger.Warn("promql fn err rate", "err", err)
	}

	writeJSON(w, http.StatusOK, metricsResponse{
		Range: rng.String(),
		Step:  step.String(),
		Series: map[string]seriesW{
			"requestRate": {Points: reqs, Unit: "req/s"},
			"latencyP95":  {Points: p95, Unit: "ms"},
			"errorRate":   {Points: errs, Unit: "req/s"},
		},
	})
}

// GET /api/v1/overview/metrics — platform-wide counters (from our own /metrics).
func (s *Server) getOverviewMetrics(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if s.prom == nil {
		http.Error(w, "metrics disabled (SPINUP_PROMETHEUS_URL not set)", http.StatusServiceUnavailable)
		return
	}
	rng, step, err := parseRange(r.URL.Query().Get("range"), r.URL.Query().Get("step"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	end := time.Now()
	start := end.Add(-rng)

	req, err := s.prom.QueryRange(r.Context(), `sum(rate(spinup_http_requests_total[1m]))`, start, end, step)
	if err != nil {
		s.logger.Warn("promql req rate", "err", err)
	}
	builds, err := s.prom.QueryRange(r.Context(), `sum(rate(spinup_builds_finished_total[5m]))`, start, end, step)
	if err != nil {
		s.logger.Warn("promql builds rate", "err", err)
	}

	writeJSON(w, http.StatusOK, metricsResponse{
		Range: rng.String(),
		Step:  step.String(),
		Series: map[string]seriesW{
			"httpRequestRate": {Points: req, Unit: "req/s"},
			"buildRate":       {Points: builds, Unit: "builds/s"},
		},
	})
}

func parseRange(rng, step string) (time.Duration, time.Duration, error) {
	if rng == "" {
		rng = "15m"
	}
	if step == "" {
		step = "15s"
	}
	r, err := time.ParseDuration(rng)
	if err != nil {
		return 0, 0, fmt.Errorf("bad range %q: %w", rng, err)
	}
	if r < time.Minute || r > 24*time.Hour {
		return 0, 0, fmt.Errorf("range must be between 1m and 24h")
	}
	s, err := time.ParseDuration(step)
	if err != nil {
		return 0, 0, fmt.Errorf("bad step %q: %w", step, err)
	}
	if s < time.Second {
		return 0, 0, fmt.Errorf("step must be >= 1s")
	}
	return r, s, nil
}
