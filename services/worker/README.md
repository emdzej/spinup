# spinup-worker

Multi-tenant `wasmtime` host for Spinup applications. One process,
N Spin applications loaded on demand, one shared `wasmtime::Engine`,
per-request WASM instantiation.

## Status

**Compiles, loads components, blocked on a WASI HTTP version mismatch.**

What works:
- Config polling against the control plane (`GET /api/v1/worker-config`)
- OCI pull via `spin registry pull` into `$HOME/.cache/spin/registry/`
- Spin lock-file (`config.json`) parse — routes, components, source digests
- Route matcher for Spin's `/prefix/...` syntax (unit tested)
- Component compilation via `wasmtime::Component::new` — accepts Component
  Model artifacts (the version-13 preamble the Go/componentize-go
  toolchain emits)
- HTTP server (axum) with `/apps/{name}/{*rest}` dispatch
- WASI Preview 2 + Preview 3 linkers, both wired
- `wasmtime::Store` + `WasiHttpView::new_incoming_request` +
  `ProxyPre::instantiate_async` + response bridging all built and reachable

What's blocked:

The Spin v4 CLI compiles guests against `wasi:http@0.3.0-rc-**2026-03-15**` —
a specific weekly release candidate of WASI HTTP Preview 3. Wasmtime 46
(current stable) provides an earlier RC. The linker rejects the guest with:

```
component imports instance `wasi:http/types@0.3.0-rc-2026-03-15`,
but a matching implementation was not found in the linker
```

Both sides are actively chasing the moving Component Model spec. To
resolve, either (a) wait for/build against a wasmtime version that provides
the 2026-03-15 RC, or (b) pin the Go builder to Spin v3.6 (which emits
p2 components that the current worker fully supports).

## Build

```bash
cd services/worker
cargo build --release
```

Requires Rust 1.83+ (for wasmtime 29). Verified on 1.96.

## Run

Standalone (dev):

```bash
export SPINUP_CONTROL_PLANE_URL=http://localhost:8080
export SPINUP_WORKER_ADDR=0.0.0.0:8000
./target/release/spinup-worker
```

Env vars:

| Variable | Default | Description |
|---|---|---|
| `SPINUP_WORKER_ADDR` | `0.0.0.0:8000` | HTTP listen address |
| `SPINUP_CONTROL_PLANE_URL` | *(unset)* | Base URL of the control plane. When set, the worker polls it for the app catalog. |
| `SPINUP_CONTROL_PLANE_TOKEN` | *(unset)* | Bearer token for the control plane's OIDC-gated API. Unset when the control plane runs with `SPINUP_DEV_INSECURE_SKIP_AUTH=true`. |
| `SPINUP_POLL_INTERVAL_SECS` | `10` | Poll interval for the config sync |
| `SPINUP_CACHE_DIR` | `/var/lib/spinup-worker` | On-disk cache root for pulled OCI artifacts |
| `RUST_LOG` | `info` | Standard tracing env filter |

## Wire protocol

The control plane's `GET /api/v1/worker-config` returns:

```json
{
  "apps": [
    {
      "id": "uuid",
      "name": "greeter",
      "language": "go",
      "imageRef": "172.19.0.2:5000/spinup/greeter:build-id",
      "description": "…",
      "functions": [
        {"name": "greeter", "route": "/..."},
        {"name": "farewell", "route": "/bye/..."}
      ]
    }
  ]
}
```

Only applications with `runtime: workerpool` and a successful build appear.
The worker calls `spin registry pull imageRef` per app and evicts anything
the control plane drops.

## Docker

```bash
# from services/worker/
docker build -t spinup/worker:latest .
docker run --rm -p 8000:8000 \
  -e SPINUP_CONTROL_PLANE_URL=http://host.docker.internal:8080 \
  spinup/worker:latest
```

## Roadmap

Immediate follow-up:

1. Wire the `wasmtime::Store` + `WasiHttpView::new_incoming_request` +
   `ProxyPre::instantiate_async` + `wasi_http_incoming_handler().call_handle`
   path (see TODO in `src/runtime.rs`).
2. LRU eviction of compiled components — currently the map grows unbounded.
3. Fair scheduling: per-app fuel/instruction limits via `Store::set_fuel`
   + `Config::consume_fuel`.
4. `/metrics` endpoint emitting `spinup_worker_requests_total`,
   `spinup_worker_active_apps`, and similar OTel metrics.
5. Chart deployment (see `deploy/helm/spinup` — worker template is a
   pending task).
