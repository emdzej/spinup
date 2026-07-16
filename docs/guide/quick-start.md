# Quick start

Get a Go function running on your laptop in about 10 minutes. This path uses [Rancher Desktop](https://rancherdesktop.io) with its built-in k3s and Wasm mode.

::: tip
The [local development](/install/local-dev) page has the full setup with troubleshooting, alternative runtimes, and more explanation. Use this quick start to see it work; use that page as your reference when things break.
:::

## 1. Install prerequisites

```bash
# Rancher Desktop (containerd + k3s + Wasm shim in one GUI app)
brew install --cask rancher

# Everything else
brew install helm go rust pnpm
```

Launch Rancher Desktop, then in Preferences:

- **Container Engine → containerd**
- **Kubernetes → enabled** (k3s)
- **Wasm mode → on** (registers the `spin` runtime class)
- Give it at least **4 CPUs / 8 GB RAM** for comfortable builds

Wait for the whale icon to settle and `kubectl get nodes` to return Ready.

## 2. Install SpinKube

```bash
# cert-manager (spin-operator's webhooks need it)
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set crds.enabled=true --wait

# spin-operator CRDs + RuntimeClass + a default executor
kubectl apply -f https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator.crds.yaml
kubectl apply -f https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator.runtime-class.yaml
kubectl apply -f https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator.shim-executor.yaml

# The operator itself
curl -sSL -o /tmp/spin-operator.tgz \
  https://github.com/spinframework/spin-operator/releases/download/v0.6.1/spin-operator-0.6.1.tgz
helm upgrade --install spin-operator /tmp/spin-operator.tgz \
  --namespace spin-operator --create-namespace --wait
```

::: warning Shim version
Rancher Desktop ships a `containerd-shim-spin-v2` binary that panics on init on some versions. If pods fail to start with `exit code 137` and containerd logs show `Failed to initialize logger`, download the upstream v0.25+ shim from [containerd-shim-spin releases](https://github.com/spinframework/containerd-shim-spin/releases) and copy it into the Lima VM at `/usr/local/containerd-shims/containerd-shim-spin-v2`. See [Local dev → Shim gotcha](/install/local-dev#rancher-shim-panic).
:::

## 3. Clone SpinUP and start the control plane

```bash
git clone <this-repo> spinup && cd spinup
pnpm install

# In one terminal — start the control plane
cd services/control-plane
SPINUP_DEV_INSECURE_SKIP_AUTH=true \
SPINUP_FUNCTIONS_NAMESPACE=spinup-functions \
SPINUP_DB_DSN=/tmp/spinup.db \
SPINUP_HTTP_ADDR=:8080 \
SPINUP_BUILDER_IMAGE_GO=spinup/builder-go:latest \
SPINUP_OCI_REGISTRY_URL=registry.spinup-functions.svc.cluster.local:5000/spinup \
go run ./cmd/control-plane
```

## 4. Provision the in-cluster registry + builder image

```bash
# Namespace + the SpinAppExecutor
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

# A plain OCI registry pods can push/pull from
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
        - { name: registry, image: registry:2, ports: [{ containerPort: 5000 }] }
---
apiVersion: v1
kind: Service
metadata: { name: registry, namespace: spinup-functions }
spec:
  type: NodePort
  selector: { app: registry }
  ports: [{ port: 5000, targetPort: 5000, nodePort: 30500 }]
YAML

# containerd on the k3s node needs to trust the cluster-DNS name.
# For Rancher Desktop:
rdctl shell -- sudo mkdir -p '/var/lib/rancher/k3s/agent/etc/containerd/certs.d/registry.spinup-functions.svc.cluster.local:5000'
rdctl shell -- sudo tee '/var/lib/rancher/k3s/agent/etc/containerd/certs.d/registry.spinup-functions.svc.cluster.local:5000/hosts.toml' >/dev/null <<'TOML'
server = "http://registry.spinup-functions.svc.cluster.local:5000"
[host."http://127.0.0.1:30500"]
  capabilities = ["pull", "resolve"]
  skip_verify = true
TOML
rdctl shell -- sudo rc-service containerd restart

# Build the Go builder image into Rancher's containerd (k8s.io namespace)
nerdctl --namespace k8s.io build -f builders/go/Dockerfile -t spinup/builder-go:latest builders/go
```

## 5. Start the UI

```bash
pnpm --filter ui dev
```

Open <http://localhost:5173>.

## 6. Create your first Application

1. Click **New application**.
2. Pick language **Go**, runtime **spinkube**, name it `hello`.
3. On the function page, edit `main.go` — the pre-populated template returns `Hello from SpinUP (Go)!`.
4. Click **Save**, then go back to the app page and click **Build & Deploy**.
5. Watch the build logs. On success, a `SpinApp` CR is applied and a pod starts.
6. Back on the function page, use the **Try it out** panel to send a request.

You should see `Hello from SpinUP (Go)!` come back.

## Where to go next

- [Writing functions](/user-guide/writing-functions) — templates and SDK basics.
- [Concepts](/guide/concepts) — Applications vs Functions vs Builds.
- [Local dev](/install/local-dev) — full setup notes and observability wiring.
- [Architecture overview](/architecture/overview) — how the platform is put together.
