# Requirements

## Cluster

- **Kubernetes 1.27+** with a container runtime that supports the [containerd-shim-spin](https://github.com/spinframework/containerd-shim-spin) shim (containerd 1.7+).
- **cert-manager** — spin-operator's webhooks need it.
- **spin-operator** — installs the `SpinApp` and `SpinAppExecutor` CRDs.
- **A container runtime with WASM support**: containerd-shim-spin-v2 registered on every node that hosts function pods.

::: tip Local development
[Rancher Desktop](https://rancherdesktop.io) 1.13+ bundles all of this out of the box when you enable Kubernetes + Wasm mode.
:::

## OCI registry

SpinUP pushes built images (one per Application per Build) to an OCI registry. Options:

- **Zot** (recommended for self-hosted) — the Helm chart can install it, or bring your own.
- **Harbor**, **GHCR**, **ECR**, **GAR**, **ACR**, **Docker Hub** — any OCI Distribution-compatible registry works.
- **An in-cluster `registry:2`** — fine for local dev, requires a containerd mirror config so nodes can pull via a cluster-DNS name.

The registry needs to be:

- Reachable from **build Jobs** in `spinup-functions` (push)
- Reachable from **containerd on nodes** (pull) — this is what usually breaks. If your registry hostname isn't resolvable from nodes' host DNS, add a mirror entry (see [Local dev](/install/local-dev#containerd-mirror)).

## Authentication

- **OIDC provider** (Auth0, Okta, Keycloak, Dex, Azure AD, Google Workspace, …). SpinUP validates the ID token and derives the user identity.
- **Or** `SPINUP_DEV_INSECURE_SKIP_AUTH=true` — disables OIDC entirely. Use for local dev only; every `/api/*` call becomes unauthenticated.

## Storage

- **State**: SQLite (single-file, backed by a PVC) or PostgreSQL. Choose at install time via `db.driver`.
- **Function sources**: stored as JSON in the state DB. No separate volume needed.

## Observability (optional but recommended)

The Helm chart can install:

- **OpenTelemetry Collector** (`observability.otelCollector.enabled`) — receives OTLP from shim-spin, runs a spanmetrics connector, exposes Prometheus scrape.
- **A Prometheus-compatible TSDB** — supply the URL via `SPINUP_PROMETHEUS_URL`. Any of VictoriaMetrics, Mimir, Thanos, or vanilla Prometheus works.
- **kube-state-metrics** — for the per-Application CPU/memory panel; the chart doesn't install it, but any standard chart works.

## Host tools (for development)

- **Go 1.22+** — build and run the control plane.
- **pnpm 9+** and **Node 20+** — build and run the UI.
- **Helm 3.14+** — install the chart.
- **kubectl** — obvious.
- **A Docker-compatible builder** — for pre-baking the builder images (nerdctl works, docker works, buildkit standalone works).

## Language runtimes for functions

Each language builder ships its own toolchain baked into the image; you don't need any of these installed locally to run functions. Just for reference:

| Language | Spin CLI | Toolchain |
|---|---|---|
| Go | v4.0.2 | Go 1.26 + `componentize-go` v0.3.3 |
| JavaScript | v4.0.2 | Node 24 + `js2wasm` plugin |
| TypeScript | v4.0.2 | Node 24 + `js2wasm` plugin |
| Rust | v4.0.2 | Rust 1.97 + `wasm32-wasip2` target |

Bumping these means rebuilding + re-pushing the builder images. See [Builders architecture](/architecture/builders) for the layout.
