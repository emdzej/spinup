# Local development setup

The end-to-end path for running SpinUP on your laptop using Rancher Desktop's k3s. If you get stuck, look at [Requirements](/install/requirements) for the "why" and this page for the "how".

## Prerequisites

```bash
brew install --cask rancher   # k3s + containerd + Wasm shim
brew install helm go rust pnpm
```

## 1. Configure Rancher Desktop

Launch it once and set in **Preferences**:

- **Container Engine**: `containerd`
- **Kubernetes**: enabled (any 1.30+ version)
- **Wasm mode**: on (adds the `spin` runtime class)
- **Resources**: 4 CPUs, 8 GB RAM

Verify:

```bash
kubectl config use-context rancher-desktop
kubectl get nodes                     # should show `lima-rancher-desktop Ready`
kubectl get runtimeclass spin         # should exist
```

## 2. Install SpinKube

```bash
# cert-manager
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set crds.enabled=true --wait

# spin-operator CRDs, runtime class, and default executor
kubectl apply -f https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator.crds.yaml
kubectl apply -f https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator.runtime-class.yaml
kubectl apply -f https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator.shim-executor.yaml

# spin-operator itself
curl -sSL -o /tmp/spin-operator.tgz \
  https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator-0.6.1.tgz
helm upgrade --install spin-operator /tmp/spin-operator.tgz \
  --namespace spin-operator --create-namespace --wait
```

## 3. Set up the functions namespace and executor

```bash
kubectl create ns spinup-functions

kubectl apply -f - <<'YAML'
apiVersion: core.spinkube.dev/v1alpha1
kind: SpinAppExecutor
metadata: { name: containerd-shim-spin, namespace: spinup-functions }
spec:
  createDeployment: true
  deploymentConfig:
    runtimeClassName: wasmtime-spin-v2
    installDefaultCACerts: true
YAML
```

## 4. In-cluster registry + containerd mirror {#containerd-mirror}

We need a registry pods can push to and containerd can pull from — both via a cluster-DNS name.

```bash
kubectl apply -f - <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata: { name: registry, namespace: spinup-functions }
spec:
  replicas: 1
  selector: { matchLabels: { app: registry } }
  template:
    metadata: { labels: { app: registry } }
    spec:
      containers:
        - name: registry
          image: registry:2
          ports: [{ containerPort: 5000, name: http }]
          volumeMounts: [{ name: data, mountPath: /var/lib/registry }]
      volumes:
        - name: data
          emptyDir: { sizeLimit: 5Gi }
---
apiVersion: v1
kind: Service
metadata: { name: registry, namespace: spinup-functions }
spec:
  type: NodePort
  selector: { app: registry }
  ports: [{ name: http, port: 5000, targetPort: 5000, nodePort: 30500 }]
YAML
```

Point containerd on the k3s node at the NodePort using a `hosts.toml`:

```bash
rdctl shell -- sudo mkdir -p '/var/lib/rancher/k3s/agent/etc/containerd/certs.d/registry.spinup-functions.svc.cluster.local:5000'
rdctl shell -- sudo tee '/var/lib/rancher/k3s/agent/etc/containerd/certs.d/registry.spinup-functions.svc.cluster.local:5000/hosts.toml' >/dev/null <<'TOML'
server = "http://registry.spinup-functions.svc.cluster.local:5000"
[host."http://127.0.0.1:30500"]
  capabilities = ["pull", "resolve"]
  skip_verify = true
TOML
rdctl shell -- sudo rc-service containerd restart
```

Why this dance: build Jobs (running in the cluster) reach the registry via its ClusterIP Service (`registry.spinup-functions.svc.cluster.local:5000`). Containerd on the node reaches the *same URL* but resolves it to `127.0.0.1:30500` via the mirror. One name, two paths, no host DNS entries needed.

## 5. Rancher shim panic {#rancher-shim-panic}

Rancher Desktop ships a `containerd-shim-spin-v2` binary that panics on init on some versions with:

```
Failed to initialize logger: IoError { context: "failed to init logger", …NotFound… }
```

Function pods will show `exit code 137` and `no runtime for "spin" is configured`. Swap it for the upstream release:

```bash
curl -sSL -o /tmp/shim.tar.gz \
  https://github.com/spinframework/containerd-shim-spin/releases/latest/download/containerd-shim-spin-v2-linux-aarch64.tar.gz
tar -xzf /tmp/shim.tar.gz -C /tmp

# Copy the binary into the Lima VM
base64 -i /tmp/containerd-shim-spin-v2 | rdctl shell -- sudo sh -c \
  'base64 -d > /usr/local/containerd-shims/containerd-shim-spin-v2.new \
   && chmod +x /usr/local/containerd-shims/containerd-shim-spin-v2.new \
   && mv /usr/local/containerd-shims/containerd-shim-spin-v2.new /usr/local/containerd-shims/containerd-shim-spin-v2'

rdctl shell -- sudo rc-service containerd restart
```

::: warning
Rancher restarts (or Wasm-mode toggles) re-copy Rancher's bundled shim over yours. If this recurs, you'll need to redo the swap. Adjust the shim path or file an issue with Rancher Desktop.
:::

## 6. Build the builder image(s)

Building into Rancher's containerd puts the image right where k3s can use it — no push required.

```bash
cd path/to/spinup
nerdctl --namespace k8s.io build -f builders/go/Dockerfile -t spinup/builder-go:latest builders/go
# Repeat for js / ts / rust as needed. Rust is slow (~15 min from scratch).
```

Verify:

```bash
nerdctl --namespace k8s.io images | grep spinup/builder
```

## 7. Run the control plane

```bash
cd services/control-plane
SPINUP_DEV_INSECURE_SKIP_AUTH=true \
SPINUP_FUNCTIONS_NAMESPACE=spinup-functions \
SPINUP_DB_DSN=/tmp/spinup.db \
SPINUP_HTTP_ADDR=:8080 \
SPINUP_BUILDER_IMAGE_GO=spinup/builder-go:latest \
SPINUP_BUILDER_IMAGE_JS=spinup/builder-js:latest \
SPINUP_BUILDER_IMAGE_TS=spinup/builder-ts:latest \
SPINUP_BUILDER_IMAGE_RUST=spinup/builder-rust:latest \
SPINUP_OCI_REGISTRY_URL=registry.spinup-functions.svc.cluster.local:5000/spinup \
go run ./cmd/control-plane
```

See [Configuration](/reference/configuration) for the complete env-var list.

## 8. Run the UI

```bash
pnpm install
pnpm --filter ui dev
# open http://localhost:5173
```

The UI proxies `/api/*` to `http://localhost:8080`.

## 9. Observability (optional)

To see per-function metrics on the function detail page, run through [Observability architecture → Enabling on k3s](/architecture/observability#enabling-on-k3s). The short version:

- Deploy the OTel Collector via the chart (or the ConfigMap under `deploy/helm/spinup/templates/otel-collector.yaml`)
- Deploy VictoriaMetrics single-node with a scrape config for the collector + kube-state-metrics + cAdvisor
- `kubectl port-forward` VM's 8428 → localhost 19090
- Restart the control plane with `SPINUP_PROMETHEUS_URL=http://localhost:19090`

## Troubleshooting

### Pods `ErrImagePull` with `no such host`

Your containerd mirror isn't in effect. Verify `hosts.toml` exists at the exact path in step 4, and that containerd was restarted after writing it.

### Pods `exit code 137` shortly after starting

Bad shim binary. See [Rancher shim panic](#rancher-shim-panic).

### Builds fail with `no such tool "componentize-go"` (Go builder)

Your Go builder image is from an older tag. Rebuild with `nerdctl --namespace k8s.io build -f builders/go/Dockerfile -t spinup/builder-go:latest builders/go --no-cache`.

### `503 no endpoints available for service "…"`

The function pod hasn't passed its readiness probe yet. Wait ~10 s and retry, or `kubectl get pods -n spinup-functions -w` to watch it come up.

### The UI shows empty metric charts

Either the control plane isn't running with `SPINUP_PROMETHEUS_URL` set, VM's port-forward died, or you haven't wired kube-state-metrics + cAdvisor scrapes. See [Observability](/architecture/observability).
