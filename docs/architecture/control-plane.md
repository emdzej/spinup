# Control plane

Location: `services/control-plane` (Go 1.22+, standard library net/http, sqlx-style SQLite/Postgres driver).

## Responsibilities

1. **HTTP API** — the surface at `/api/v1/*`. Documented at [HTTP API reference](/reference/http-api).
2. **Persistence** — Applications, Functions, Sources, Builds, and a per-Function multi-file source blob.
3. **Build orchestration** — synthesize `spin.toml`, pack source into a Secret, create a K8s Job, watch it.
4. **Deploy** — apply `SpinApp` CRs so spin-operator materialises pods.
5. **Invocation** — relay UI-initiated requests to the pod via the K8s API server's service proxy.
6. **Log streaming** — chunked HTTP endpoint tailing pod stderr via the K8s pod-log API.
7. **Metrics** — server-side PromQL against a Prometheus-compatible TSDB, exposed as `{t, v}` time series for the UI.
8. **Own telemetry** — Prometheus `/metrics` endpoint with request counters + build outcome counters.

## Package layout

```
services/control-plane/
├── cmd/control-plane/main.go        # env → Config → dependency wiring → http.Server
└── internal/
    ├── auth/                        # OIDC verifier + dev-skip middleware
    ├── builder/                     # Job orchestration, manifest synth, build watcher
    ├── config/                      # env vars → typed Config struct
    ├── httpapi/                     # HTTP handlers (one per resource)
    │   ├── applications.go
    │   ├── functions.go
    │   ├── source.go
    │   ├── builds.go
    │   ├── invoke.go
    │   ├── logs.go
    │   ├── metrics.go
    │   ├── worker.go
    │   └── server.go                # mux + middleware wiring
    ├── promql/                      # thin Prometheus HTTP client
    ├── proxy/                       # K8s API server service-proxy wrapper (for spinkube invoke)
    ├── spinapp/                     # dynamic-client wrapper for SpinApp CR CRUD
    ├── store/                       # DB abstraction (sqlite.go, postgres.go)
    └── telemetry/                   # own /metrics + HTTPMiddleware for request stats
```

## Storage schema

Migrations run at startup — see `internal/store/sqlite.go`. Tables:

- `tenants` (id, name)
- `applications` (id, tenant_id, name, language, runtime, description, created_at)
- `functions` (id, application_id, name, route, created_at)
- `sources` (function_id, files_json, updated_at)
- `builds` (id, application_id, status, image_ref, error, created_at, finished_at)

The `sources.files_json` column stores the full `{filename: content}` map as JSON. Reasonable for source sizes typical in HTTP functions (kilobytes, occasionally tens of kilobytes). If you need larger blobs, migrate to blob storage in a follow-up.

The build ID is a **UUID with dashes stripped** (`newID()` in `internal/builder/builder.go`) — same as the OCI tag. Immutable, content-addressable, DNS-1123-safe.

## HTTP middleware stack

```
mux
 └── /api/*
      ├── telemetry.Metrics.HTTPMiddleware      (adds request/duration counters)
      └── auth.Verifier.Middleware              (OIDC verify OR dev-skip)
           └── individual handlers
```

The `statusRecorder` in `internal/telemetry/metrics.go` implements `http.Flusher` explicitly — needed because embedded-interface method promotion in Go doesn't forward `Flush()` from the underlying `ResponseWriter`. Without this, streaming handlers (like `/logs?follow=true`) silently buffer through the middleware.

## Build watcher

For each `POST /builds`, the control plane spawns a background goroutine that watches the corresponding K8s Job:

```
create Secret(src-{id})
create Job(build-{id})
loop:
    poll Job status
    stream pod logs into DB
    on Complete → apply SpinApp CR → mark build succeeded
    on Failed → capture error → mark build failed
    on Deadline → mark build failed
```

Watcher lifetime is tied to the CP process — restarting the CP mid-build leaves the Job orphaned. On CP startup, any `running` builds are marked `failed` with `error: "control plane restarted during build"` (implemented at `internal/builder/builder.go` startup).

## SpinApp client

`internal/spinapp/spinapp.go` wraps the K8s **dynamic client** — we don't import the SpinKube Go module (avoids a version pin) and instead read/write `SpinApp` as `unstructured.Unstructured`.

Server-side apply is used for the Apply operation, so the control plane's `fieldManager: "spinup-control-plane"` co-exists with other actors (e.g. an operator adding autoscaling annotations later).

## Invoke routing

`internal/httpapi/invoke.go` calls `proxy.Client.Invoke(...)`, which hits the K8s API server's service-proxy path (`/api/v1/namespaces/{ns}/services/{app-name}/proxy/...`). No pod IP or DNS resolution needed from the CP.

Responses funnel through `writeInvokeResponse(...)` which:

- Decides base64 vs UTF-8 encoding based on `Content-Type` + `utf8.Valid(body)`
- Enforces the 1 MiB response cap (`proxy.MaxResponseBody`)
- Emits the shared `invokeResponseDTO`

## Log streaming

`internal/httpapi/logs.go` — key subtleties:

- Uses `PodLogsByLabel(selector)` — resolves the pod by `core.spinkube.dev/app-name={app.Name}` (SpinKube's label convention).
- Writes an initial `\n` byte and flushes **before** entering the pod-log read loop. This kicks Go's chunked transfer encoding immediately so intermediate proxies (Vite dev, nginx/istio prod) start streaming to the client without buffering until the first pod-log byte arrives.
- Uses `context.Context` from the request — client disconnect closes the pod-log stream via the `defer stream.Close()`.

## Metrics endpoints

Per-Application (`/api/v1/applications/{id}/metrics`) queries cAdvisor + kube-state-metrics:

```promql
sum(rate(container_cpu_usage_seconds_total{namespace="spinup-functions", container!="", container!="POD"}[5m])
    * on(namespace, pod) group_left
    kube_pod_labels{namespace="spinup-functions", label_core_spinkube_dev_app_name="{app.Name}"})
```

Per-Function (`/api/v1/applications/{id}/functions/{fnId}/metrics`) queries OTel spanmetrics:

```promql
sum(rate(traces_span_metrics_calls_total{span_kind="SPAN_KIND_SERVER", http_route="{fn.Route}"}[2m]))
histogram_quantile(0.95, sum by (le) (rate(traces_span_metrics_duration_milliseconds_bucket{…}[2m])))
```

Both are constructed in `internal/httpapi/metrics.go`. The UI never sees PromQL — swap the TSDB by editing that one file.

## Configuration

All configuration flows in through environment variables validated at startup by `internal/config/config.go`. See [Configuration reference](/reference/configuration) for the complete list.

Notable ones:

| Var | Purpose |
|---|---|
| `SPINUP_HTTP_ADDR` | Listen address (default `:8080`) |
| `SPINUP_DB_DRIVER` + `SPINUP_DB_DSN` | Where to persist state |
| `SPINUP_OIDC_ISSUER_URL` + `_CLIENT_ID` | OIDC token verification |
| `SPINUP_DEV_INSECURE_SKIP_AUTH` | Escape hatch for local dev |
| `SPINUP_FUNCTIONS_NAMESPACE` | Where builds and SpinApps land |
| `SPINUP_OCI_REGISTRY_URL` | Where builders push |
| `SPINUP_BUILDER_IMAGE_{GO,JS,TS,RUST}` | Per-language builder images |
| `SPINUP_PROMETHEUS_URL` | Enable metrics endpoints |
