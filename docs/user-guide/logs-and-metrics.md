# Logs & metrics

## Runtime logs

Every function detail page has a **Runtime logs** card that streams the underlying pod's stderr in real time.

Under the hood:

- The UI opens `GET /api/v1/applications/{appId}/logs?follow=true&tail=200` on the control plane
- The CP resolves the pod via label selector `core.spinkube.dev/app-name={app.Name}` and calls the K8s pod-log API with `Follow: true`
- The response streams line-by-line back through the CP → Vite proxy → browser as chunked HTTP

For multi-function Applications, the stream includes lines from all components (they share one pod). Spin's trigger tracing lines are tagged with `component_id="{fn.name}"` so you can filter visually. User-code stderr isn't tagged — you distinguish by content.

## Build logs

The Builds table on the application page lists every build attempt with status. Click a row to stream that build's logs, live if it's still running.

Same mechanism as runtime logs, but scoped to the build Job's pod. Retained as long as the pod exists (K8s Job GC applies).

## Metrics

Two panels ship with the UI:

### Application-level: Resource usage

Shown on the application detail page. Two line charts:

- **CPU** — sum of `container_cpu_usage_seconds_total` (rate over the selected range), keyed by pod, joined to `kube_pod_labels{label_core_spinkube_dev_app_name=…}`. Unit: cores.
- **Memory** — sum of `container_memory_working_set_bytes`, same join. Unit: bytes.

Data source: cAdvisor via each kubelet's `/metrics/cadvisor` + kube-state-metrics for the label join.

### Function-level: Traffic

Shown on the function detail page. Three line charts:

- **Request rate** — `sum(rate(traces_span_metrics_calls_total{span_kind="SPAN_KIND_SERVER",http_route="{fn.route}"}[2m]))`. Unit: req/s.
- **p95 latency** — `histogram_quantile(0.95, …_duration_milliseconds_bucket…)`. Unit: ms.
- **5xx rate** — same as request rate but filtered by `http_response_status_code=~"5.."`. Unit: req/s.

Data source: the OTel Collector's spanmetrics connector — it turns Spin trigger HTTP spans into RED metrics keyed by `http.route` (per function) and `http.response.status_code` (for error rate).

### Range selector

Every metric panel has a Range dropdown: 5 min / 15 min / 1 h / 6 h. Points get plotted at:

| Range | Step |
|---|---|
| 5 min | 5 s |
| 15 min | 15 s |
| 1 h | 30 s |
| 6 h | 2 min |

Panels auto-refresh every 5-10 s.

## Setting up the metrics stack

The panels only populate if the control plane has `SPINUP_PROMETHEUS_URL` set to a working Prometheus-compatible TSDB. The chart doesn't install a TSDB — bring your own or deploy one alongside.

Minimum viable local setup:

- **OTel Collector** with the spanmetrics connector (installed by the chart when `observability.otelCollector.enabled=true`)
- **kube-state-metrics** with `--metric-labels-allowlist=pods=[core.spinkube.dev/app-name,…]` so the pod label survives
- **VictoriaMetrics** (or Prometheus) scraping the collector's `:9464` + kube-state-metrics + kubelet's `/metrics/cadvisor`

Full manifests: [Observability architecture → Enabling on k3s](/architecture/observability#enabling-on-k3s).

## Distributed traces

The bundled OTel Collector exports traces to `debug` (stdout) by default — good for confirming they flow, not useful for exploring.

To send traces to a real backend:

- **Jaeger / Tempo / Zipkin**: add an `otlp` exporter to the collector's ConfigMap pointing at the backend, and route it in the `traces` pipeline.
- **Datadog / Honeycomb / New Relic**: add their respective exporter, plus any API key via a Secret.
- **Grafana Cloud**: use the `otlphttp` exporter with the tenant-specific endpoint.

The chart doesn't expose this yet — patch the collector ConfigMap post-install, or fork the template.

## Metrics you might want that aren't wired

- **Build duration** — the control plane emits `spinup_builds_finished_total{outcome}` on its own `/metrics` endpoint. Point Prometheus at that (via a scrape config on the CP Service, port 8080) to graph build outcomes and duration.
- **HTTP request counts by user** — not implemented. Would need identity to flow into the OTel resource attributes.

## Troubleshooting empty charts

1. `curl http://localhost:8080/healthz` — CP responding?
2. `curl http://localhost:8080/api/v1/applications/{id}/functions/{fnId}/metrics?range=5m` — raw response has data?
3. `curl http://localhost:19090/api/v1/label/__name__/values` (assuming your VM port-forward) — does the TSDB have `traces_span_metrics_*`?
4. Is the SpinAppExecutor's `otel.exporter_otlp_endpoint` set to a reachable collector? `kubectl get spinappexecutor containerd-shim-spin -n spinup-functions -o yaml`
5. Did you invoke the function recently enough? Spanmetrics counters age out; `rate([2m])` returns 0 if no requests in the last 2 min.
