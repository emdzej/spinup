# Scaling roadmap — from SpinKube-per-function to real WASM-native serverless

## Where we are today

Every SpinUP function creates a **SpinApp CR → Deployment → Pod** running `containerd-shim-spin`. Familiar Kubernetes patterns, but N functions = N always-on pods. This wastes Spin's headline capability (per-request WASM instantiation in ~1 ms) on pod-scoped scheduling costs.

Idle-pod overhead is small in absolute terms (~15–40 MB RSS per function), and per-request latency is already sub-5 ms once a pod is warm. So the current design is **fine for self-hosted dev / staging / small teams** — it's the wrong architecture only if we're chasing "true public multi-tenant serverless".

## The vision: two runtime flavours

SpinUP should ultimately support **two backends** the operator picks at install time:

- **`runtime: spinkube`** *(default, what exists today)* — one SpinApp/Deployment per function. Safe, isolated, K8s-native, matches how most teams operate. Scale-to-zero optional via KEDA (Option B).
- **`runtime: workerpool`** *(future)* — shared wasmtime worker pods that dynamically load any function's component from OCI. No per-function K8s objects. Sub-100 ms cold start, true per-request pricing model, N functions on M pods where M is small and load-scaled.

Both share the rest of the stack (control plane, UI, builder, OCI registry, observability). Only the *scheduling* backend differs.

---

## Status snapshot

| Option | Status | Notes |
|---|---|---|
| **A — Component packing** | ✅ **shipped** | Implemented as the first-class `Application` entity + multi-function packing. Every Application is one SpinApp / one pod / one wasmtime process with N components sharing it. See section below for the ORIGINAL sketch — the shipped shape is cleaner. |
| **B — KEDA scale-to-zero** | ⏳ queued | Next scaling move. ~1–2 days. |
| **C — Worker pool** | 🕰️ queued (later) | Multi-week project. |

## Option A — Component packing (single SpinApp, many components) [SHIPPED]

**What it is.** Spin's manifest supports N `[[trigger.http]]` blocks in one app. Group functions (per-tenant, per-project, per-tag) into a single SpinApp so they share one pod.

**Impact.** N functions → 1 pod per group. Shared wasmtime process. Sub-millisecond per-request instantiation preserved. No new components in the platform.

**Design sketch.**
- New concept: `FunctionGroup` (or reuse `tenant_id` as the group). Store `group_id` on `functions` row.
- Builder produces one WASM per function as today (or per group, TBD).
- Deploy step no longer emits one SpinApp per function; instead emits/updates the group's SpinApp with N trigger blocks and N components (each referencing its own OCI blob via `source = { url = "oci://...", digest = "..." }`).
- Route convention: `route = "/fn/{function-name}/..."` per trigger; the router path-strips.

**Trade-offs.**
- Shared failure domain — one bad component can crash the pod for everyone in the group.
- Shared resource limits — one hot function starves the rest.
- Adding a function requires updating the SpinApp spec → rolling the pod → brief unavailability for the whole group. Mitigate with `strategy: RollingUpdate` + `maxUnavailable: 0`.
- Rebuild-per-function still costs the same; only the *deploy* step packs.

**Effort.** ~1 week: schema change, packing logic in the SpinApp writer, UI group management (list functions in a group, deploy button per group).

**When to do it.** When idle-pod overhead becomes visibly wasteful — e.g., 20+ low-traffic functions in a dev cluster. Or as a "team plan" tier in a multi-tenant product.

**As shipped**: introduced `Application` as a first-class entity above `Function` (rather than the ad-hoc `group_id` this section originally proposed). `POST /api/v1/applications` auto-creates a first Function; multi-function apps are just adding more functions via `POST /api/v1/applications/{id}/functions`. Builder synthesizes a multi-component `spin.toml` and packs one OCI image per App per build. See `internal/builder/manifest.go` and the `applications` + `functions` schema in `internal/store/sqlite.go`.

---

## Option B — Scale to zero with KEDA

**What it is.** Keep the per-function Deployment, but let KEDA's HTTP scaler scale it to zero on idle. Pod boots on first request; subsequent requests hit the warm pod at normal speed.

**Impact.** N idle functions → 0 pods. Wake-on-request. Cold start dominated by pod boot (~1–3 s), not wasmtime.

**Design sketch.**
- Chart: install KEDA as a dependency (or expect it pre-installed).
- Chart: templates for `ScaledObject` — one per SpinApp when a `hibernate` flag is set on the function.
- Backend: `Function.hibernate: bool` (or in a config sub-object). When true, the control plane also emits a `ScaledObject`.
- SpinApp already supports `enableAutoscaling`; we set `minReplicas: 0`, `maxReplicas: N`.
- Router / gateway: on request to a hibernated function, KEDA's http-add-on holds the connection while the pod wakes.

**Trade-offs.**
- Cold start is not "milliseconds" — it's pod-boot slow. Fine for spiky/cron/low-freq functions, wrong for latency-sensitive APIs.
- KEDA HTTP add-on is still in beta at time of writing; check its stability against target k8s version before committing.
- Adds an operator dependency to the chart.

**Effort.** ~1–2 days: chart plumbing, `hibernate` toggle on the Function DTO, UI checkbox in the create form.

**When to do it.** Quick win for handling many rarely-used functions cheaply. Recommended as the first move if pod-cost concerns come up before we're ready for Option C.

---

## Option C — Worker pool with dynamic component loading

**What it is.** The Fermyon Cloud / wasmCloud pattern. A small fleet (2–8 pods) of long-running `wasmtime` hosts. A stateless router dispatches by function name to any worker. Each worker keeps an LRU cache of compiled components in memory; on cache miss, pulls the WASM blob from OCI (Zot) and compiles it (~50–100 ms one-time), then invokes.

**Impact.** N functions → M pods (small, constant). Deploy = update router routing table, ~50 ms. Cold invoke ~50–100 ms (cache miss), warm ~1–5 ms. No per-function K8s objects at all.

### Architecture

```
                    ┌────────────────┐
   /fn/{name}  ────▶│ spinup-router  │  Envoy or small Go proxy
                    └───────┬────────┘
                            │  Header: X-Spin-Fn-Ref: oci://.../hello@sha256:...
                            ▼
                    ┌─────────────────────────────────────────┐
                    │  spinup-worker  (K8s Deployment, N pods)│
                    │  ┌──────────────────────────────────┐   │
                    │  │  wasmtime host process (Rust)    │   │
                    │  │   - LRU cache: precompiled .cwasm│   │
                    │  │   - WASI HTTP proxy binding      │   │
                    │  │   - OCI resolver → Zot           │   │
                    │  └──────────────────────────────────┘   │
                    └─────────────────────────────────────────┘

  Control-plane on deploy:
    - build + push to Zot (unchanged)
    - PATCH the router config: {function name → OCI ref by digest}
    - NO SpinApp CR, NO pod scheduling
```

### Components to build

1. **`spinup-worker`** — Go binary using `wasmtime-go`'s Component Model bindings. Serves an internal HTTP endpoint that accepts `X-Spin-Fn-Ref` header + request body, translates to `wasi:http/incoming-handler`, returns the guest response. Manages an LRU of pre-compiled modules (`wasmtime.Module.Serialize()` → `.cwasm` on disk cache, in-memory `Module` cache above that).

   **Why Go over Rust**: consistency with the rest of the backend, faster team velocity. `wasmtime-go` supports the Component Model and WASIp2 today; the API lags Rust by weeks-to-months for cutting-edge features. If we hit a real feature gap (unlikely for HTTP triggers), rewrite the worker in Rust — swap only that binary, no wire-format changes. Fermyon Cloud and wasmCloud chose Rust largely for ecosystem depth (`wasmtime-wasi-http` official crate, `oci-distribution`, etc.); we can achieve the same in Go with a bit more glue code.

2. **`spinup-router`** — new lightweight service. Reads a routing table (from control-plane, watched via a CRD or a ConfigMap or a gRPC stream). For `/fn/{name}/...`, looks up the OCI ref, forwards to the worker Service with the header set. Handle rate limiting + tenant identity here.

3. **Control-plane changes**:
   - `runtime` config knob: `spinkube` (today) or `workerpool` (new).
   - When `workerpool`, deploy path skips SpinApp CR creation; instead PATCHes the router's routing table (via a `SpinUPRoute` CRD we define, or a k8s Endpoint/ConfigMap).
   - Delete function → route removed → next cache eviction cleans the worker.

4. **Chart changes**: `runtime` value; conditional deployment of router + worker pool.

### Design decisions to pin

- **Router transport**: HTTP forward (simple) vs gRPC (fewer overhead per call, per-call metadata clean). Start with HTTP.
- **Cache invalidation**: OCI digest-based (immutable content addressing). When control-plane updates the routing table with a new digest, workers naturally miss and repull. Old digests age out of LRU.
- **Multi-tenancy isolation**: wasmtime already sandboxes each instance. For CPU/memory limits, use wasmtime's `Store` limits + fuel metering. Per-tenant fair scheduling is a bigger topic.
- **Warm-up on deploy**: control-plane can optionally issue an internal `/warm/{ref}` to each worker after deploy so first user request isn't the cache-miss path.
- **Language boundary**: worker in Go (with `wasmtime-go`) to match the rest of the backend. Fall back to Rust only if we hit a Component Model feature gap in `wasmtime-go`.

### Effort estimate

- MVP worker in Go (single-tenant, HTTP forward, in-memory cache only, no fuel metering): ~2 weeks focused work.
- MVP router + control-plane integration: ~1 week.
- Chart + observability + polish + smoke tests: ~1 week.
- **Total: ~4 weeks of one focused engineer** for a demoable end-to-end.
- Production readiness (multi-tenancy limits, ejection on OOM, security review, load testing): another 2–4 weeks.
- If we hit a `wasmtime-go` limitation and need to rewrite the worker in Rust: add ~1–2 weeks. Low probability for HTTP-only components.

### When to do it

When the platform's user model shifts from "self-hosted for one team" to "multi-tenant serverless offering." Or if we want to make a compelling public demo of Spin's true throughput/cost profile.

---

## Recommended sequencing

1. **Done** — SpinKube-based product with Applications as first-class packing units (Option A). Observability, invoke proxy, source import/export, Monaco editor, per-function route editing all landed.
2. **Next quick win** when pod cost matters — **Option B** (KEDA scale-to-zero, opt-in per Application).
3. **Later, big investment** when we're ready to be a real serverless platform — **Option C** (worker pool). Ship as `runtime: workerpool` chart flavour alongside `runtime: spinkube`. Same UI, same builder pipeline, same OCI registry, same observability. Only the deploy backend swaps.

## Open questions to answer before committing to Option C

- How do we handle **stateful function requirements** (KV stores, sqlite via `spin-sqlite`)? SpinKube pods have their own attached PVCs today; a worker pool means shared state must live outside the pod (which is a good pattern for serverless anyway).
- **Cold-start budget**: can we get the wasmtime cache miss under 50 ms end-to-end including OCI pull? First measurements should be a `wasmtime pre-compile + serialize` + local `zot` pull benchmark before we commit.
- **Fair scheduling**: how do we prevent a badly-behaved function from eating a worker's CPU? Fuel metering is coarse; async runtime cooperative scheduling helps but isn't a hard limit.
- **Node placement**: does the worker pool need `runtimeClassName: wasmtime-spin-v2`? Probably not — we host wasmtime directly, not via containerd-shim-spin.

## References

- Fermyon Cloud architecture (public info): <https://www.fermyon.com/blog/fermyon-cloud-architecture>
- wasmCloud host architecture: <https://wasmcloud.com/docs/concepts/hosts>
- wasmtime Component Model + WASI: <https://docs.wasmtime.dev/api/wasmtime/component/index.html>
- SpinKube autoscaling (built-in HPA): <https://www.spinkube.dev/docs/topics/autoscaling/>
- KEDA HTTP add-on: <https://github.com/kedacore/http-add-on>
