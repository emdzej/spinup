# Agent guide

Written for LLM coding agents (Claude Code, Cursor, Aider, Codex, …) working on this repo. This file complements the human-facing [README](./README.md) and [docs/](./docs) with the operational knowledge that isn't obvious from reading the code.

If you're a human reading this: everything here is true, but you probably want [docs/architecture/overview.md](./docs/architecture/overview.md) instead.

## Project summary

SpinUP is a Kubernetes-native cloud-functions platform for [Spin](https://spinframework.dev) HTTP components. Users author source in a browser (Monaco editor); the platform builds a WASM component, pushes to an OCI registry, and applies a `SpinApp` CR that spin-operator materializes as a pod with `runtimeClassName: wasmtime-spin-v2`. An alternative `workerpool` runtime shares a single wasmtime process across many apps.

**Alpha software.** APIs, chart values, and schemas change without notice.

## Directory map

```
apps/ui/                      SvelteKit 2 + Svelte 5 + Vite 8 + Monaco. Pnpm workspace.
builders/{go,js,ts,rust}/     Per-language builder Docker images (Spin CLI + toolchain + scaffold).
deploy/helm/spinup/           Helm chart. Templates for CP, executor, Zot, worker, OTel Collector, Istio.
docs/                         VitePress documentation site (Markdown + Mermaid).
services/
  control-plane/              Go module. HTTP API, K8s Job orchestration, SpinApp Apply, log stream, PromQL.
  worker/                     Rust binary. Multi-tenant wasmtime host for `runtime: workerpool` (alpha).
```

Everything else at the repo root is standard scaffolding (`go.work`, `pnpm-workspace.yaml`, `package.json`).

## Where to change what

- **Add an HTTP endpoint**: handler in `services/control-plane/internal/httpapi/*.go`, route registered in `server.go`.
- **Change a build command** (per language): `services/control-plane/internal/builder/manifest.go` — the `writeComponent(...)` switch. Also the corresponding `builders/{lang}/Dockerfile` if the toolchain changes.
- **Add a config env var**: `services/control-plane/internal/config/config.go`. Also update [docs/reference/configuration.md](./docs/reference/configuration.md).
- **Change the DB schema**: `services/control-plane/internal/store/sqlite.go` (and mirror in `postgres.go`). Also `internal/store/store.go` for the Go structs. Also [docs/reference/database.md](./docs/reference/database.md).
- **UI templates for new function code**: `apps/ui/src/lib/templates.ts`.
- **UI API client**: `apps/ui/src/lib/api.ts`.
- **UI types matching CP DTOs**: `apps/ui/src/lib/types.ts`. Manually kept in sync — no codegen.
- **Chart values**: `deploy/helm/spinup/values.yaml`. Templates use `.Values.*` extensively.

## Local development

### One-time setup

- Rancher Desktop with **containerd + Kubernetes + Wasm mode**. Give it 4 CPUs / 8 GB.
- `pnpm install` at the repo root — installs UI, docs, and any other Node workspaces.
- Install cert-manager + spin-operator: see [docs/install/local-dev.md](./docs/install/local-dev.md#2-install-spinkube).
- Install the containerd registry mirror once (survives Rancher restarts if you don't toggle Wasm mode): [docs/install/local-dev.md#containerd-mirror](./docs/install/local-dev.md#containerd-mirror).

### Running the stack

Three processes, three terminals (or use tmux/zellij):

```bash
# 1. Control plane
cd services/control-plane
SPINUP_DEV_INSECURE_SKIP_AUTH=true \
SPINUP_FUNCTIONS_NAMESPACE=spinup-functions \
SPINUP_DB_DSN=/tmp/spinup.db \
SPINUP_BUILDER_IMAGE_GO=spinup/builder-go:latest \
SPINUP_BUILDER_IMAGE_JS=spinup/builder-js:latest \
SPINUP_BUILDER_IMAGE_TS=spinup/builder-ts:latest \
SPINUP_BUILDER_IMAGE_RUST=spinup/builder-rust:latest \
SPINUP_OCI_REGISTRY_URL=registry.spinup-functions.svc.cluster.local:5000/spinup \
SPINUP_PROMETHEUS_URL=http://localhost:19090 \
go run ./cmd/control-plane

# 2. UI (from repo root)
pnpm --filter ui dev

# 3. Port-forward VictoriaMetrics for metrics endpoints (if you enabled observability)
kubectl -n spinup port-forward svc/victoriametrics 19090:8428
```

Common port map:

| Port | Owner |
|---|---|
| 5173 | Vite (UI) |
| 8080 | Control plane HTTP API |
| 19090 | Port-forward to in-cluster VictoriaMetrics :8428 |
| 4317 / 4318 / 9464 | Port-forwards to in-cluster OTel Collector (grpc/http/prom) — as needed |
| 18000 | Port-forward to in-cluster spinup-worker :8000 (workerpool runtime) |
| 30500 | NodePort of the in-cluster `registry:2` (host side of the containerd mirror) |

**Port-forwards die on any Rancher restart** — restart them and the CP will pick up again.

### Building the builder images

```bash
NERDCTL="/Applications/Rancher Desktop.app/Contents/Resources/resources/darwin/bin/nerdctl"
"$NERDCTL" --namespace k8s.io build -f builders/go/Dockerfile   -t spinup/builder-go:latest   builders/go
"$NERDCTL" --namespace k8s.io build -f builders/js/Dockerfile   -t spinup/builder-js:latest   builders/js
"$NERDCTL" --namespace k8s.io build -f builders/ts/Dockerfile   -t spinup/builder-ts:latest   builders/ts
"$NERDCTL" --namespace k8s.io build -f builders/rust/Dockerfile -t spinup/builder-rust:latest builders/rust
```

Rust is slow (~10-15 min from scratch). Kick it in background if you don't need it right away. **Never** run multiple heavy builds in parallel — Rancher's Lima VM will starve and kubectl becomes unresponsive.

### Type-check / build / test

```bash
# Go
cd services/control-plane && go build ./... && go vet ./...

# Rust (heavy — the worker's Cargo.toml pulls wasmtime + hyper + tokio)
cd services/worker && cargo build

# UI
pnpm --filter ui check      # svelte-check
pnpm --filter ui build      # production bundle

# Docs
pnpm --filter spinup-docs build   # catches broken links & missing pages

# Helm chart
helm lint deploy/helm/spinup --set dnsName=x --set oidc.issuerUrl=https://x/ --set oidc.clientId=x --set oci.registryUrl=x
```

## Environmental gotchas

### Microsoft Defender kills docker when I do container-analysis-shaped things

On the operator's machine, mdatp (Microsoft Defender for Endpoint) kills `com.docker.backend` when it observes activity that looks like reverse-engineering:

- `docker cp <container>:/path/to/binary /host/path` — extracting binaries
- `strings binary` or similar analysis on extracted files
- Tight loops of `docker exec` / `docker inspect` / `docker port`

**Never do those.** Use `kubectl exec` (via `kubectl`, not `docker`) to inspect inside pods. Read files in place with `cat`. Batch docker calls. If you truly need a binary out for analysis, ask the operator to pause Defender first.

Full context: memory file `defender-docker-triggers` at `~/.claude/projects/-Users-mjaskols-Projects-my-spinup/memory/defender_docker_triggers.md`.

### Rancher's bundled containerd-shim-spin panics on init

`Failed to initialize logger: IoError { … NotFound … }` from `containerd-shimkit-0.1.1`. Function pods exit with 137 and containerd logs show `no runtime for "spin" is configured`.

Fix: replace `/usr/local/containerd-shims/containerd-shim-spin-v2` with the upstream v0.25+ release. Recipe: [docs/install/local-dev.md#rancher-shim-panic](./docs/install/local-dev.md#rancher-shim-panic).

**Rancher restarts (and Wasm mode toggles) clobber the swap.** You'll need to redo it.

### Rancher's VM has more resources on paper than in practice

The Lima VM defaults are aggressive on CPU (all node CPUs) but conservative on memory. Rust builds inside the VM (worker, big user apps) can starve kube-apiserver → kubectl becomes unresponsive → SSH mux refuses new sessions. When that happens:

1. Wait a few minutes to let the build finish, or
2. `rdctl shutdown && rdctl start` (~40s) to hard-restart the VM and abort the build

**Never** kick off `nerdctl build` on the worker without first ensuring nothing else critical is happening.

### Port-forwards are ephemeral

`kubectl port-forward` dies when:

- The target pod restarts
- Rancher restarts
- The client process (your terminal) exits
- Network hiccups (rare)

Metrics endpoints on the CP silently return empty data when the VM port-forward is down. If a user reports "empty charts", check port-forwards first.

### `docker` on this machine points at Docker Desktop, not Rancher

`/usr/local/bin/docker` is a symlink to Docker.app. Docker Desktop is unreliable here (see Defender). Prefer:

- `nerdctl` at `/Applications/Rancher Desktop.app/Contents/Resources/resources/darwin/bin/nerdctl` — Rancher's containerd CLI
- `kubectl` for anything inside the cluster
- Only use `docker` when you specifically need a docker daemon and know Docker Desktop is up

### The VictoriaMetrics deployment uses `emptyDir`

Data is lost on VM restart. Not a problem for dev — invoke a function again and metrics come back. If reproducing a metric-related bug across sessions, either script fresh traffic in the repro OR give VM a PVC.

### The in-cluster `registry:2` also uses `emptyDir` (5 GiB limit)

Same story. Images are wiped on pod restart. In practice, trigger a rebuild and continue. In the chart's Zot mode, storage is a PVC.

## Known blockers

### WASI HTTP RC version drift

The `spinup-worker` (Rust, wasmtime 46) fails to link Spin-built guests because:

- Wasmtime 46 provides `wasi:http@0.3.0` (stable)
- Spin v4.0.2 (and `canary` as of 2026-07-16) emits `wasi:http@0.3.0-rc-2026-03-15` (dated pre-release)

Component Model semver treats these as incompatible.

**Do not touch `services/worker/src/runtime.rs` unless the operator explicitly asks.** The code path is correct; the only issue is the upstream version drift. Full analysis and paths forward: memory file `wasi-http-version-gotcha`.

Quick check when reopening this: `strings $(spin path)/spin | grep wasi:http | grep 0.3`. If the output shows `@0.3.0` cleanly, the ecosystem has caught up and we're unblocked with zero code changes.

## Anti-patterns

Things that seem reasonable but bite here:

- **Committing `services/worker/target/`** — 4 GB of Rust artifacts. `.gitignore` covers it; don't `git add -f` past it.
- **Editing `services/worker/src/runtime.rs`** — see WASI HTTP blocker above.
- **Adding OTel export config to the workerpool worker** — the worker's already wired for OTel; adding more config just makes the panic surface earlier.
- **Writing new documentation as `docs/*.md` at the repo root** — they get orphaned. Docs go under `docs/` in a VitePress-friendly section and get sidebar entries in `docs/.vitepress/config.ts`.
- **Bypassing the CP to write directly to K8s** — the CP is the source of truth for `SpinApp` CRs. If you `kubectl apply` a SpinApp directly, the next build will overwrite your change.
- **Using `docker cp` to extract things from containers** — Defender triggers, see above.
- **Committing without running `helm lint` after chart changes** — silent template errors surface at install time on someone else's cluster.
- **Adding OpenAPI/gRPC schemas "for later"** — the HTTP API is small and hand-documented. If codegen becomes worthwhile, ask first.

## Conventions

### Go

- `slog` for logging, never `log.Println`.
- `context.Context` first parameter for anything that hits I/O.
- No custom error types unless the caller pattern-matches. `fmt.Errorf("action: %w", err)` is enough.
- Handlers stream where they can: `http.Flusher.Flush()` on chunked responses. `statusRecorder` in `internal/telemetry/metrics.go` implements `Flush` explicitly — needed because embedded-interface method promotion in Go doesn't forward `Flush()`.
- No config outside `internal/config/config.go`. Everything is env-driven and validated once at startup.
- Store methods take `context.Context`, return `(T, error)`. Nothing panics.
- Use `unstructured.Unstructured` for K8s CRs when the CRD's Go bindings would add a version pin (we do this for SpinApp).

### UI / Svelte

- Svelte 5 runes (`$state`, `$derived`, `$effect`). No stores. No `$:` reactive statements.
- Types in `apps/ui/src/lib/types.ts`, mirroring CP DTOs. No codegen; sync manually.
- API client (`apps/ui/src/lib/api.ts`) is the only place that constructs fetch URLs. Pages import functions from there.
- No auth headers in the client — the browser sends the OIDC cookie / token via Vite proxy → CP.
- Charts use `MetricChart.svelte` (uPlot). Metrics data comes from the CP as `[[t, v], ...]` arrays.

### Rust (worker)

- Only touch this crate if asked. See WASI HTTP blocker.
- Tokio + Axum + Hyper 1 + Wasmtime 46. Feature flags in Cargo.toml pin `p2` + `p3`.
- `tracing` crate for logs — never `println!`.
- The router keeps `Arc<AppEntry>` and holds compiled components. LRU eviction is TODO.

### Chart

- Values in `values.yaml` have inline comments explaining defaults and interactions. Preserve them on edits.
- Every template guards behind a feature flag (`.Values.worker.enabled`, `.Values.observability.otelCollector.enabled`, `.Values.oci.mode`). Nothing gets unconditionally installed except the minimum: CP, executor, namespace.

### Docs

- Prose is direct. No "You might be wondering…" or "It's important to note that…" filler.
- Every diagram is Mermaid inside a ```` ```mermaid ```` fence. No ASCII art (VitePress renders Mermaid natively via the plugin).
- Every reference-style doc has a "not implemented" section listing what's queued but not done. Better a paper trail than "why doesn't feature X work?".

## Memory system

The operator uses Claude Code with the persistent memory system at `~/.claude/projects/-Users-mjaskols-Projects-my-spinup/memory/`. Notable entries:

| Slug | Contents |
|---|---|
| `project-goal` | High-level goal statement. |
| `architecture-decisions` | OIDC-only, SQLite/Postgres, central gateway, CronJob scheduling. |
| `language-scope` | Go for services, Svelte+TS for UI. V1 langs: Go, JS/TS, Rust. |
| `observability-plan` | OTel /metrics → Vector → VictoriaMetrics; UI queries VM via CP. |
| `scaling-roadmap` | Three options queued; details in `docs/scaling-roadmap.md`. Do not implement without ask. |
| `wasi-http-version-gotcha` | The workerpool blocker. Contains recovery instructions. |
| `docker-deployment-path` | Queued: docker-compose-only SpinUP. Do not start without ask. |
| `defender-docker-triggers` | What NOT to do that makes Defender kill Docker. |

Consult these before starting new work. Add new memories when you learn something durable about the environment or a "why" that isn't obvious from code.

## Common tasks

### "Add a new HTTP endpoint"

1. Handler in `services/control-plane/internal/httpapi/<resource>.go`.
2. Route in `server.go` under `api.HandleFunc(...)` in the right block.
3. If it needs a new store method, add to `internal/store/store.go` (interface) + `sqlite.go` (implementation) + mirror in `postgres.go`.
4. If it needs new config, add to `internal/config/config.go`.
5. UI client method in `apps/ui/src/lib/api.ts`; types in `types.ts`.
6. Docs: [docs/reference/http-api.md](./docs/reference/http-api.md).

### "Bump the Spin CLI version"

1. Edit `ARG SPIN_VERSION=vX.Y.Z` in all four `builders/*/Dockerfile`.
2. Rebuild the affected images (see above).
3. Try one build per language.
4. Update [docs/install/requirements.md](./docs/install/requirements.md) with the new versions.

Test path: create a fresh Application per language and build it end-to-end. Test both `runtime: spinkube` and, if unblocked, `runtime: workerpool`.

### "Add a helm value"

1. Add to `deploy/helm/spinup/values.yaml` with a comment.
2. Use in the appropriate template under `templates/`.
3. Run `helm lint deploy/helm/spinup --set …` with any required scalars.
4. Add the value to [docs/reference/chart-values.md](./docs/reference/chart-values.md).

### "Investigate a failing build"

1. `curl http://localhost:8080/api/v1/applications/{id}/builds/{buildId}` — check the `error` field.
2. `curl http://localhost:8080/api/v1/applications/{id}/builds/{buildId}/logs` — full pod log.
3. `kubectl -n spinup-functions get pod -l job-name=build-{buildId}` — pod state (Completed vs Error vs ImagePullBackOff).
4. `kubectl -n spinup-functions describe pod …` — events.
5. If the image can't be pulled: is `spinup/builder-{lang}:latest` in `nerdctl --namespace k8s.io images`? Did you build it?

## Testing philosophy

- **Go**: table-driven unit tests where interesting; integration tests hit a real K8s API server (envtest / kind). Package-under-test dictates.
- **UI**: `svelte-check` for types; no component tests today. Rely on manual UI passes.
- **Rust worker**: `cargo test` for pure Rust; integration deferred until WASI HTTP unblocks.
- **Chart**: `helm lint` + `helm template` diff review. No dedicated test harness.
- **Docs**: `vitepress build` catches broken links.

Don't add heavy test infrastructure without asking. This is early alpha; the reward-to-friction ratio isn't there yet.

## Style & tone in prose

- **Direct**. No "As you can see," "It's worth mentioning," or "you might notice."
- **Concrete**. Every claim is a fact about this repo, not a general principle.
- **Falsifiable**. Prefer statements someone could disprove ("The CP writes X to Y") over vague ones ("The CP handles state well").
- **No emoji** unless the operator explicitly asks.
- **Code snippets are runnable.** No pseudocode. No `...` placeholders where a real value would be short.

## Licensing

- Source available under [PolyForm Noncommercial 1.0.0](./LICENSE). Commercial use requires a separate agreement — see [LICENSE-COMMERCIAL.md](./LICENSE-COMMERCIAL.md).
- Contributions require CLA acceptance ([CLA.md](./CLA.md)). Any code you author or accept in a PR must be relicensable by the Maintainer.
- **Do not add dependencies with GPL/AGPL/SSPL licenses.** The CI license workflow will fail. If you must add a dep with an OR-license expression that includes GPL, ensure the alternative is on the allowlist and add an override in `services/worker/about.toml` (Rust) or the equivalent config.
- When you add or bump a dependency, run `bash scripts/gen-third-party-notices.sh` and commit the regenerated `THIRD-PARTY-NOTICES.md` files. CI diffs against the committed versions and fails otherwise.

## Things the operator has explicitly opted out of

Do not do any of these unless the operator asks:

- Docker-only deployment path (memory: `docker-deployment-path`).
- Scaling roadmap options B (KEDA) and C (worker pool implementation beyond what's already there).
- Multi-cluster / multi-region support.
- OpenAPI / gRPC schema generation.
- CI/CD wiring (GitHub Actions, etc.) — the operator hasn't set that up.
- Package for other Kubernetes distributions beyond what the chart already handles.
- Any operation that requires `docker cp` of container binaries.

## When in doubt

- Look at recent commits: `git log --oneline -30`.
- Read [docs/architecture/overview.md](./docs/architecture/overview.md) for how pieces fit.
- Read the memory files for context on decisions.
- Ask the operator. Bad guesses cost more than a clarification round-trip.
