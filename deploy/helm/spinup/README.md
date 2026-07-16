# spinup Helm chart

Deploys the Spinup control plane to a Kubernetes cluster that already runs SpinKube (`SpinApp` / `SpinAppExecutor` CRDs installed).

## Prerequisites

- Kubernetes 1.28+
- SpinKube operator installed (`spinkube-shim-executor` and CRDs). See <https://www.spinkube.dev/docs/install/>.
- Istio installed with an `istio-ingressgateway` in the cluster (matches the pattern used in `mnmsy-infra`).
- An OIDC provider you can point Spinup at.
- A TLS certificate available as a Kubernetes Secret in the release namespace, referenced by `istio.tls.credentialName`.

## Install

```bash
helm install spinup ./deploy/helm/spinup \
  --namespace spinup --create-namespace \
  --set dnsName=spinup.example.com \
  --set oidc.issuerUrl=https://login.example.com/ \
  --set oidc.clientId=spinup
```

## Ingress alternatives

The chart ships Istio manifests because that matches this platform's convention. Two other options if you're not on Istio:

- **Native `networking.k8s.io/v1` Ingress**: set `istio.enabled=false` and add an `Ingress` resource pointing `spinup.example.com` at the `spinup-control-plane` Service. Fine for simple clusters; loses per-request policy hooks.
- **Gateway API (`gateway.networking.k8s.io`)**: set `istio.enabled=false` and add a `Gateway` + `HTTPRoute` — cleaner future-proof choice, but adoption is uneven across managed clusters.

## Database

- `db.driver=sqlite` (default): control-plane writes to a file on a PVC. Fine for single-replica deployments; loses HA.
- `db.driver=postgres`: set `db.postgres.secretName` / `db.postgres.secretKey` to point at a Secret containing a `postgres://…` DSN. Required for multi-replica.

## OCI registry

The builder pushes function images to an OCI registry; SpinKube pulls from the same registry. Two modes:

**External registry** (default, `oci.mode: external`) — use an existing registry:

```bash
helm install spinup ./deploy/helm/spinup \
  --set oci.registryUrl=ghcr.io/your-org/spinup-fns \
  --set oci.auth.existingSecret=ghcr-pull-secret          # optional
```

Auth precedence: `oci.auth.existingSecret` beats `oci.auth.inline.enabled`. Both fall back to anonymous if unset. `existingSecret` must be a `kubernetes.io/dockerconfigjson` Secret in the functions namespace. For inline, provide a `dockerconfigjson` string in `oci.auth.inline.dockerconfigjson` — the chart creates the Secret for you.

**In-cluster Zot** (`oci.mode: zot`) — chart deploys a Zot registry alongside Spinup:

```bash
helm install spinup ./deploy/helm/spinup --set oci.mode=zot
# oci.registryUrl is derived from the Zot Service DNS automatically.
```

Zot is deployed with **plain HTTP + anonymous access** — appropriate for single-team dev/staging. For production Zot: add TLS via cert-manager and configure Zot's auth in a custom values override.

Image-arch note: Zot doesn't ship a multi-arch tag. Override for arm64:
```yaml
oci:
  zot:
    image:
      repository: ghcr.io/project-zot/zot-linux-arm64
      tag: v2.1.2
```

## What's not in this chart yet

- SpinAppExecutor resource (belongs to the SpinKube install, not to Spinup)
- UI as a separate Deployment (currently assumed to be served by the control-plane binary)
- OpenTelemetry `/metrics` scrape configuration (planned)
