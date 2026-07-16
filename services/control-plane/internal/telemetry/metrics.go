// Package telemetry wires OpenTelemetry with a Prometheus exporter so the
// control plane serves a `/metrics` endpoint that VictoriaMetrics (or any
// Prometheus-compatible scraper) can consume.
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	promapi "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const scope = "github.com/emdzej/spinup/services/control-plane"

type Metrics struct {
	registry *promapi.Registry
	meter    metric.Meter

	HTTPRequests metric.Int64Counter
	HTTPDuration metric.Float64Histogram

	FunctionsCreated metric.Int64Counter
	FunctionsDeleted metric.Int64Counter
	DeploysApplied  metric.Int64Counter
	BuildsStarted   metric.Int64Counter
	BuildsFinished  metric.Int64Counter
	BuildDuration   metric.Float64Histogram
}

// Init sets up the OTel MeterProvider with a Prometheus exporter and returns
// a Metrics bundle plus the /metrics HTTP handler.
func Init(ctx context.Context, serviceVersion string) (*Metrics, http.Handler, error) {
	reg := promapi.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	exp, err := prometheus.New(prometheus.WithRegisterer(reg))
	if err != nil {
		return nil, nil, fmt.Errorf("prom exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("spinup-control-plane"),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resource: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exp),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	meter := mp.Meter(scope)
	m := &Metrics{registry: reg, meter: meter}

	if m.HTTPRequests, err = meter.Int64Counter("spinup_http_requests_total",
		metric.WithDescription("HTTP requests handled by the control plane")); err != nil {
		return nil, nil, err
	}
	if m.HTTPDuration, err = meter.Float64Histogram("spinup_http_request_duration_seconds",
		metric.WithDescription("HTTP request duration"), metric.WithUnit("s")); err != nil {
		return nil, nil, err
	}
	if m.FunctionsCreated, err = meter.Int64Counter("spinup_functions_created_total",
		metric.WithDescription("Functions created")); err != nil {
		return nil, nil, err
	}
	if m.FunctionsDeleted, err = meter.Int64Counter("spinup_functions_deleted_total",
		metric.WithDescription("Functions deleted")); err != nil {
		return nil, nil, err
	}
	if m.DeploysApplied, err = meter.Int64Counter("spinup_deploys_applied_total",
		metric.WithDescription("SpinApp CR applies invoked by the control plane")); err != nil {
		return nil, nil, err
	}
	if m.BuildsStarted, err = meter.Int64Counter("spinup_builds_started_total",
		metric.WithDescription("Build Jobs started")); err != nil {
		return nil, nil, err
	}
	if m.BuildsFinished, err = meter.Int64Counter("spinup_builds_finished_total",
		metric.WithDescription("Build Jobs that reached a terminal state")); err != nil {
		return nil, nil, err
	}
	if m.BuildDuration, err = meter.Float64Histogram("spinup_build_duration_seconds",
		metric.WithDescription("Time from build start to terminal state"), metric.WithUnit("s")); err != nil {
		return nil, nil, err
	}

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
	return m, handler, nil
}

// HTTPMiddleware wraps a handler with request-count and duration metrics.
// Labels: method, route pattern (falls back to the URL path if pattern is empty), status class.
func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}
		attrs := metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("route", route),
			attribute.String("status", statusClass(rw.status)),
		)
		m.HTTPRequests.Add(r.Context(), 1, attrs)
		m.HTTPDuration.Record(r.Context(), time.Since(start).Seconds(), attrs)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying ResponseWriter if it implements http.Flusher.
// Needed because embedding http.ResponseWriter as an interface only promotes
// the interface's own methods — not additional interfaces like http.Flusher
// that the concrete type happens to satisfy. Without this, streaming handlers
// (e.g. /logs?follow=true) silently buffer.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}
