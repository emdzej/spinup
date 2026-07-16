# Production install (Helm)

For real deployments, install SpinUP as a Helm chart into a dedicated namespace. This gets you the control plane as an in-cluster Deployment, optional Zot registry, and optional OTel collector — all managed together.

## Prerequisites

You need the pieces from [Requirements](/install/requirements) in place: cert-manager, spin-operator (CRDs + operator), and a `containerd-shim-spin` shim on nodes.

## Install

```bash
helm upgrade --install spinup deploy/helm/spinup \
  --namespace spinup --create-namespace \
  --set dnsName=spinup.example.com \
  --set oidc.issuerUrl=https://login.example.com/ \
  --set oidc.clientId=spinup \
  --set oci.mode=zot \
  --set observability.otelCollector.enabled=true
```

The chart creates:

- **`spinup` namespace** (Deployment, Service, ServiceAccount for the control plane)
- **`spinup-functions` namespace** (function pods land here; separate namespace = independent RBAC and NetworkPolicy)
- **SpinAppExecutor** in `spinup-functions` (with OTel binding wired to the collector when enabled) — set `spinAppExecutor.create=false` when your cluster already has a SpinKube executor you want SpinApps to use
- **Istio Gateway + VirtualService** for the public route (`dnsName`) — disable via `istio.enabled=false` if you use Nginx/Traefik/native Ingress
- **Zot registry** if `oci.mode=zot` (PVC-backed, plain HTTP; add TLS/auth for real use)
- **OTel collector** if `observability.otelCollector.enabled=true` — with a spanmetrics connector already configured

Full value reference: [Helm chart values](/reference/chart-values).

## Common variations

### Reuse an existing SpinKube executor

If the cluster already has a `SpinAppExecutor` you want SpinApps to reference (e.g. installed by your platform team), skip chart-side creation and point SpinUP at the existing executor's name:

```bash
helm upgrade --install spinup deploy/helm/spinup \
  --set spinAppExecutor.create=false \
  --set spinAppExecutor.name=containerd-shim-spin
```

`SpinAppExecutor` is namespace-scoped, so the executor must live in `spinup-functions` (or wherever your `functionsNamespace.name` points). Copy the manifest across namespaces if needed.

### Bring your own OCI registry

```bash
helm upgrade --install spinup deploy/helm/spinup \
  --set oci.mode=external \
  --set oci.registryUrl=ghcr.io/your-org/spinup \
  --set oci.auth.existingSecret=ghcr-pull-secret
```

The `existingSecret` must be a `kubernetes.io/dockerconfigjson` Secret in the `spinup-functions` namespace. Build Jobs mount it at `/root/.docker/config.json` so `spin registry push` picks up the credentials.

### PostgreSQL instead of SQLite

```bash
kubectl create secret generic spinup-db \
  --namespace spinup \
  --from-literal=dsn='postgres://user:pass@host/db?sslmode=require'

helm upgrade --install spinup deploy/helm/spinup \
  --set db.driver=postgres \
  --set db.postgres.secretName=spinup-db \
  --set db.postgres.secretKey=dsn
```

### Wire OTel to your own backend

The bundled collector exports to `debug` (stdout) for traces and logs, and `prometheus` (:9464) for metrics. To send to a real backend, override the collector ConfigMap or add exporters via a values override:

```yaml
observability:
  otelCollector:
    enabled: true
    # (chart doesn't expose this yet — patch the ConfigMap post-install)
```

## Upgrading

Bump the chart version, re-run `helm upgrade`. Rolling update semantics apply:

- **Control plane**: `maxUnavailable: 0`, rolling — API is briefly served by the old + new pods in parallel.
- **Function pods**: unaffected. The control plane doesn't touch SpinApp CRs on upgrade unless you also rebuild.

## Uninstall

```bash
helm uninstall spinup --namespace spinup
kubectl delete namespace spinup spinup-functions
```

Function images remain in the OCI registry; delete them separately if needed. The chart doesn't auto-delete PVCs (SQLite data, Zot storage) — clean those up with `kubectl delete pvc -n spinup --all` if you want a fresh start.

## What the chart doesn't handle

- **cert-manager + spin-operator** install (chart-level dependency but not a sub-chart yet — install them separately, see [Requirements](/install/requirements))
- **kube-state-metrics** for the app-level metric panel (any standard chart works, e.g. `prometheus-community/kube-state-metrics`)
- **A Prometheus-compatible TSDB** — deploy VictoriaMetrics, Mimir, or Prometheus separately and point `SPINUP_PROMETHEUS_URL` at it
- **Backups of the state DB** — take a `kubectl cp` snapshot of the SQLite PVC, or use your PostgreSQL provider's backup story
