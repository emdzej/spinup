# Helm chart values

Reference for `deploy/helm/spinup/values.yaml`.

## Top-level

| Key | Default | Purpose |
|---|---|---|
| `dnsName` | `spinup.example.com` | Public DNS used by the Istio VirtualService for external routing. |

## Functions namespace

| Key | Default | Purpose |
|---|---|---|
| `functionsNamespace.name` | `spinup-functions` | Namespace for function pods, SpinApps, and build Jobs. |
| `functionsNamespace.create` | `true` | Let the chart create the namespace. Set false if you provision it externally. |

## SpinAppExecutor

| Key | Default | Purpose |
|---|---|---|
| `spinAppExecutor.create` | `true` | Create the executor in `functionsNamespace`. Set to `false` when the cluster already has one there (platform-managed SpinKube install) — SpinApps still reference `spinAppExecutor.name`. |
| `spinAppExecutor.name` | `containerd-shim-spin` | Executor name (referenced by every SpinApp we apply). |
| `spinAppExecutor.runtimeClassName` | `wasmtime-spin-v2` | RuntimeClass the executor asks the kubelet for. |
| `spinAppExecutor.installDefaultCACerts` | `true` | Mount the K8s cluster's CA bundle so guest code can reach TLS hosts. |

## Observability

| Key | Default | Purpose |
|---|---|---|
| `observability.otelCollector.enabled` | `false` | Deploy an OpenTelemetry Collector alongside the control plane. |
| `observability.otelCollector.image.repository` | `otel/opentelemetry-collector-contrib` | Collector image. |
| `observability.otelCollector.image.tag` | `0.111.0` | Collector version. |
| `observability.otelCollector.resources` | `{cpu 100m/1, mem 256Mi/512Mi}` | Standard resource block. |

When enabled, the SpinAppExecutor also gets its `otel.exporter_otlp_endpoint` pointed at the collector's cluster-DNS Service (`spinup-otel-collector.{ns}.svc.cluster.local:4318`).

## Worker

| Key | Default | Purpose |
|---|---|---|
| `worker.image.tag` | `0.1.0` | Worker version. |
| `worker.replicas` | `1` | Number of worker pods. Scale up under load. |
| `worker.resources` | `{cpu 100m/2, mem 128Mi/2Gi}` | Standard resource block. |
| `worker.controlPlaneToken` | *(empty)* | Bearer token the worker uses to authenticate against `/api/v1/worker-config`. Unset when the CP runs with dev-skip. |

When enabled, `SPINUP_WORKER_URL` on the CP is set to the worker Service DNS automatically.

## OCI registry

| Key | Default | Purpose |
|---|---|---|
| `oci.mode` | `external` | `external` (bring your own) or `zot` (chart deploys Zot). |
| `oci.registryUrl` | *(empty)* | Push prefix. Required when `mode=external`. Auto-computed for `mode=zot`. |
| `oci.auth.existingSecret` | *(empty)* | Name of a `dockerconfigjson` Secret to mount into build Jobs. |
| `oci.auth.inline.enabled` | `false` | Let the chart create the Secret from an inline JSON string. |
| `oci.auth.inline.dockerconfigjson` | *(empty)* | Plain JSON string; the chart base64-encodes it. Do not commit this to git. |

### Zot sub-config (when `oci.mode=zot`)

| Key | Default | Purpose |
|---|---|---|
| `oci.zot.image.repository` | `ghcr.io/project-zot/zot-linux-amd64` | Zot image (pick amd64 or arm64). |
| `oci.zot.image.tag` | `v2.1.2` | Zot version. |
| `oci.zot.service.name` | `zot` | Service name. |
| `oci.zot.service.port` | `5000` | Registry port. |
| `oci.zot.persistence.enabled` | `true` | PVC for storage. |
| `oci.zot.persistence.size` | `5Gi` | PVC size. |
| `oci.zot.persistence.storageClassName` | *(empty)* | Storage class. |

## Control plane

| Key | Default | Purpose |
|---|---|---|
| `controlPlane.image.repository` | `ghcr.io/emdzej/spinup-control-plane` | CP image. |
| `controlPlane.image.tag` | `0.1.0` | CP version. |
| `controlPlane.replicas` | `1` | Number of CP pods. |
| `controlPlane.resources` | `{cpu 100m/500m, mem 128Mi/512Mi}` | Standard resource block. |

## OIDC

| Key | Default | Purpose |
|---|---|---|
| `oidc.issuerUrl` | *(empty)* | OIDC discovery URL. Required. |
| `oidc.clientId` | *(empty)* | Application client / audience. Required. |
| `oidc.audience` | (= clientId) | Override the expected audience. |
| `oidc.redirectUrl` | (= `https://{dnsName}/auth/callback`) | Fully-qualified callback URL registered with the IdP. Must match byte-for-byte. |
| `oidc.clientSecret.existingSecret` | *(empty)* | Name of a Secret holding the confidential-client secret. Preferred. |
| `oidc.clientSecret.secretKey` | `client-secret` | Key inside `existingSecret`. |
| `oidc.clientSecret.value` | *(empty)* | Inline client secret (dev only — never commit real values). Ignored when `existingSecret` is set. |

## Authorization

| Key | Default | Purpose |
|---|---|---|
| `authz.requiredRoles` | `[]` | Any-of match against the ID token `roles` claim. Empty list = every authenticated user is allowed. Configure the IdP to project the assigned role into a top-level `roles` array — Keycloak: client role mapper; Entra: App Role; Auth0: post-login action. |

## Database

| Key | Default | Purpose |
|---|---|---|
| `db.driver` | `sqlite` | `sqlite` or `postgres`. |
| `db.sqlite.dsn` | `/var/lib/spinup/spinup.db` | Path inside the CP pod. |
| `db.sqlite.persistence.enabled` | `true` | Create a PVC for the SQLite file. |
| `db.sqlite.persistence.size` | `2Gi` | PVC size. |
| `db.postgres.secretName` | *(empty)* | Name of a Secret with the DSN. |
| `db.postgres.secretKey` | `dsn` | Key inside the Secret. |

## Istio ingress

| Key | Default | Purpose |
|---|---|---|
| `istio.enabled` | `true` | Install a Gateway + VirtualService. |
| `istio.gatewaySelector.istio` | `ingressgateway` | Match label on the Istio Gateway. |
| `istio.tls.credentialName` | `spinup-gw-credential` | Name of a cert-manager-issued cert Secret. |

Disable this and install your own Ingress / native Gateway API if you don't run Istio.
