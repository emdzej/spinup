# Control-plane environment variables

All configuration goes through env vars, validated at startup by `internal/config/config.go`. Anything with a default is optional; anything without a default is required for the corresponding feature to work.

## HTTP

| Variable | Default | Description |
|---|---|---|
| `SPINUP_HTTP_ADDR` | `:8080` | Listen address for the HTTP API server. |

## Persistence

| Variable | Default | Description |
|---|---|---|
| `SPINUP_DB_DRIVER` | `sqlite` | `sqlite` or `postgres`. |
| `SPINUP_DB_DSN` | `spinup.db` | For SQLite: file path. For Postgres: connection string (`postgres://user:pass@host/db?sslmode=require`). |

The control plane runs schema migrations at startup. Both drivers use the same migration set.

## Auth

| Variable | Default | Description |
|---|---|---|
| `SPINUP_OIDC_ISSUER_URL` | *(unset)* | OIDC issuer discovery URL. Required unless dev-skip is on. |
| `SPINUP_OIDC_CLIENT_ID` | *(unset)* | OIDC client (audience). Required unless dev-skip is on. |
| `SPINUP_OIDC_AUDIENCE` | (= `SPINUP_OIDC_CLIENT_ID`) | Override the expected audience. |
| `SPINUP_DEV_INSECURE_SKIP_AUTH` | *(unset)* | Set to `true` to disable OIDC verification entirely. **Local dev only.** |

## Kubernetes

| Variable | Default | Description |
|---|---|---|
| `SPINUP_KUBECONFIG` | *(unset)* | Path to a kubeconfig for local dev. Ignored in-cluster (uses in-cluster config). |
| `SPINUP_FUNCTIONS_NAMESPACE` | `spinup-functions` | Namespace where function pods, SpinApp CRs, and build Jobs land. |

## Builders

| Variable | Default | Description |
|---|---|---|
| `SPINUP_BUILDER_IMAGE_GO` | `spinup/builder-go:latest` | Image for Go builds. |
| `SPINUP_BUILDER_IMAGE_JS` | `spinup/builder-js:latest` | Image for JS builds. |
| `SPINUP_BUILDER_IMAGE_TS` | `spinup/builder-ts:latest` | Image for TS builds. |
| `SPINUP_BUILDER_IMAGE_RUST` | `spinup/builder-rust:latest` | Image for Rust builds. |

## OCI registry

| Variable | Default | Description |
|---|---|---|
| `SPINUP_OCI_REGISTRY_URL` | `ttl.sh/spinup` | Prefix used to construct the OCI ref: `{prefix}/{app-name}:{build-id}`. |
| `SPINUP_OCI_AUTH_SECRET` | *(unset)* | Name of a `kubernetes.io/dockerconfigjson` Secret in `SPINUP_FUNCTIONS_NAMESPACE`. Mounted into build Jobs at `/root/.docker/config.json` when set. |

## Runtimes

| Variable | Default | Description |
|---|---|---|
| `SPINUP_WORKER_UI_URL` | (= `SPINUP_WORKER_URL`) | Alternate URL shown in the UI's invoke card if the worker is reachable via a different public URL. |

## Observability

| Variable | Default | Description |
|---|---|---|
| `SPINUP_PROMETHEUS_URL` | *(unset)* | Base URL of a Prometheus-compatible TSDB. When set, `/api/v1/*/metrics` endpoints are enabled. |
| `SPINUP_PUBLIC_BASE_URL` | *(unset)* | Externally-reachable base URL used to compute Application `publicUrl` (e.g., `https://spinup.example.com`). |

## UI

| Variable | Default | Description |
|---|---|---|
| `SPINUP_UI_STATIC_DIR` | *(unset)* | Filesystem path to a SvelteKit build (typically `apps/ui/build`). Empty = serve the UI baked into the CP binary at compile time. Set this in local dev when running `go run` to point at a locally-built UI. |

## Startup validation

The control plane refuses to start if:

- Neither OIDC nor dev-skip is configured
- `SPINUP_DB_DRIVER` isn't `sqlite` or `postgres`
- The Postgres DSN is invalid (checked lazily on first connect)

It logs a WARN on startup for dev-skip so you can spot it in the logs.
