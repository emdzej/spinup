# control-plane

Go service that fronts the Spinup UI and orchestrates SpinKube. Currently a scaffold — only the HTTP shell, OIDC verifier, and SQLite store are wired up.

## Configuration

All configuration is via environment variables.

| Variable                       | Required | Default             | Description                                                   |
|--------------------------------|----------|---------------------|---------------------------------------------------------------|
| `SPINUP_HTTP_ADDR`             | no       | `:8080`             | HTTP listen address                                           |
| `SPINUP_OIDC_ISSUER_URL`       | **yes**  |                     | OIDC issuer (e.g. `https://login.example/`)                   |
| `SPINUP_OIDC_CLIENT_ID`        | **yes**  |                     | OIDC client ID                                                |
| `SPINUP_OIDC_AUDIENCE`         | no       | `CLIENT_ID`         | Expected token audience                                       |
| `SPINUP_DB_DRIVER`             | no       | `sqlite`            | `sqlite` or `postgres`                                        |
| `SPINUP_DB_DSN`                | no       | `spinup.db`         | sqlite file path or postgres connection URL                   |
| `SPINUP_FUNCTIONS_NAMESPACE`   | no       | `spinup-functions`  | Namespace where `SpinApp` resources are created               |
| `SPINUP_KUBECONFIG`            | no       |                     | kubeconfig path for local dev (in-cluster config wins)        |

## Run locally

```bash
export SPINUP_OIDC_ISSUER_URL=https://your-idp/
export SPINUP_OIDC_CLIENT_ID=spinup-dev
go run ./cmd/control-plane
```

## Endpoints

All `/api/*` routes require an OIDC-issued Bearer token.

- `GET /healthz` — liveness/readiness, no auth
- `GET /api/v1/functions` — list functions
- `POST /api/v1/functions` — create function metadata `{name, language, description?}` (name must be DNS-1123)
- `GET /api/v1/functions/{id}` — function metadata + cluster deployment status
- `POST /api/v1/functions/{id}/deploy` — apply a `SpinApp` from `{image, replicas?}`
- `DELETE /api/v1/functions/{id}` — deletes the `SpinApp` and the DB row

## Smoke test against a local cluster

Assumes:
- A kubeconfig context pointing at a cluster with the SpinKube operator installed (`SpinApp` / `SpinAppExecutor` CRDs present, the `wasmtime-spin-v2` RuntimeClass installed, and containerd-shim-spin available on nodes — via kwasm-operator on kind).
- The functions namespace exists (`kubectl create ns spinup-functions`).
- A `SpinAppExecutor` named `containerd-shim-spin` exists **in that namespace** (SpinAppExecutor is namespace-scoped — spin-operator's default install puts it in `default`, so you need to re-apply it into `spinup-functions`). The Spinup Helm chart handles this automatically.

```bash
# For local dev without a real OIDC provider, skip token verification.
# NEVER set this outside local dev.
export SPINUP_DEV_INSECURE_SKIP_AUTH=true
export SPINUP_FUNCTIONS_NAMESPACE=spinup-functions
go run ./cmd/control-plane

# In another shell:
BASE=http://localhost:8080

# 1. Register the function.
curl -sSf -X POST $BASE/api/v1/functions \
  -H "content-type: application/json" \
  -d '{"name":"hello","language":"rust","description":"smoke test"}'
# → {"id":"...","name":"hello","language":"rust",...}
ID=<id from above>

# 2. Deploy it using a public SpinKube example image (no builder needed yet).
curl -sSf -X POST $BASE/api/v1/functions/$ID/deploy \
  -H "content-type: application/json" \
  -d '{"image":"ghcr.io/spinframework/containerd-shim-spin/examples/spin-rust-hello:v0.19.0","replicas":1}'

# 3. Check status.
curl -sSf $BASE/api/v1/functions/$ID
kubectl -n spinup-functions get spinapp hello -o yaml

# 4. Invoke through a port-forward on the SpinKube-generated Service.
kubectl -n spinup-functions port-forward svc/hello 18080:80 &
curl http://localhost:18080/hello   # → Hello world from Spin!

# 5. Tear down.
curl -sSf -X DELETE $BASE/api/v1/functions/$ID
```

With OIDC configured instead of the dev bypass, replace the `SPINUP_DEV_INSECURE_SKIP_AUTH=true` line with `SPINUP_OIDC_ISSUER_URL=... SPINUP_OIDC_CLIENT_ID=...` and pass `-H "Authorization: Bearer $TOKEN"` on every `/api/*` call.

## TODO

- Postgres store implementation (`internal/store/postgres.go`)
- Builder Job orchestration (`internal/builder`)
- CronJob management for scheduled triggers
- Function versions + deploy history endpoints
- Log streaming from function pods
- OpenTelemetry `/metrics` endpoint + VictoriaMetrics scrape
