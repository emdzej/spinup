# Observability

Every part of the stack emits signals — the interesting question is where they end up.

## Signal sources

| Component | Emits |
|---|---|
| Function pod (via `containerd-shim-spin`) | OTLP traces + logs (per-request spans, structured log events); pod stderr |
| Function pod (via kubelet + cAdvisor) | Container CPU / memory / network / filesystem metrics |
| Control plane | Own `/metrics` (request counts, build outcome counts); slog stderr |
| OTel Collector | Own `/metrics` on `:8888`; spanmetrics-derived metrics on `:9464` |

## The bundled pipeline

The Helm chart's `observability.otelCollector.enabled=true` installs an OpenTelemetry Collector configured for SpinUP:

```yaml
receivers:
  otlp:
    protocols:
      grpc:  { endpoint: 0.0.0.0:4317 }
      http:  { endpoint: 0.0.0.0:4318 }

processors:
  batch:
  resource:
    attributes:
      - { key: collector.instance, value: spinup, action: insert }

connectors:
  spanmetrics:
    histogram:
      explicit:
        buckets: [2ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s]
    dimensions:
      - { name: http.route }
      - { name: http.request.method }
      - { name: http.response.status_code }
    exemplars: { enabled: true }

exporters:
  prometheus: { endpoint: 0.0.0.0:9464, send_timestamps: true }
  debug:      { verbosity: basic }

service:
  pipelines:
    traces:
      receivers:  [otlp]
      processors: [batch, resource]
      exporters:  [debug, spanmetrics]
    metrics:
      receivers:  [otlp, spanmetrics]
      processors: [batch, resource]
      exporters:  [prometheus]
    logs:
      receivers:  [otlp]
      processors: [batch, resource]
      exporters:  [debug]
```

Key idea: the **spanmetrics connector** converts every incoming HTTP request span into a counter (`traces_span_metrics_calls_total`) and a latency histogram (`traces_span_metrics_duration_milliseconds_*`), keyed by the dimensions above.

::: warning OTLP protocol
Use **HTTP/protobuf (port 4318)**, not gRPC (4317), for the shim's export. The shim's OTLP gRPC exporter defaults to TLS and doesn't downgrade to plain h2c even when the URL scheme is `http://`, so the gRPC path silently drops data. The chart's default endpoint uses `:4318`.
:::

## Routing traces to the collector

The SpinAppExecutor CR has a native `deploymentConfig.otel.exporter_otlp_endpoint` field. When set, spin-operator injects `OTEL_EXPORTER_OTLP_ENDPOINT` on every function pod.

Chart template (rendered when `observability.otelCollector.enabled=true`):

```yaml
apiVersion: core.spinkube.dev/v1alpha1
kind: SpinAppExecutor
spec:
  createDeployment: true
  deploymentConfig:
    runtimeClassName: wasmtime-spin-v2
    installDefaultCACerts: true
    otel:
      exporter_otlp_endpoint: "http://spinup-otel-collector.spinup.svc.cluster.local:4318"
```

::: tip
`service.name` on the exported spans is always `spin` — the shim doesn't have a per-app service-name knob today. Distinguish apps by looking at the `component_id` attribute (present on the shim's tracing events) or the `http.route` dimension (unique per function).
:::

## What the UI plots

### Per-function traffic (function detail page)

Three panels, all backed by the CP's `/api/v1/applications/{id}/functions/{fnId}/metrics`:

- **Request rate**: `sum(rate(traces_span_metrics_calls_total{span_kind="SPAN_KIND_SERVER", http_route="{fn.route}"}[2m]))`
- **p95 latency**: `histogram_quantile(0.95, sum by (le) (rate(traces_span_metrics_duration_milliseconds_bucket{…}[2m])))`
- **5xx rate**: `sum(rate(…{http_response_status_code=~"5.."}[2m]))`

The 2m rate window is picked to match the collector's spanmetrics flush interval (15s) and give ~8 data points per query.

### Per-application resource usage (application detail page)

Two panels backed by `/api/v1/applications/{id}/metrics`, joining cAdvisor with kube-state-metrics:

- **CPU**: `sum(rate(container_cpu_usage_seconds_total{…}[5m]) * on(namespace, pod) group_left kube_pod_labels{label_core_spinkube_dev_app_name="{name}"})`
- **Memory**: `sum(container_memory_working_set_bytes{…} * on(namespace, pod) group_left kube_pod_labels{…})`

The join lets us key by SpinKube's `core.spinkube.dev/app-name` pod label, which requires kube-state-metrics's `--metric-labels-allowlist` to include that label.

## Enabling on k3s {#enabling-on-k3s}

The chart's collector deployment covers the OTel side, but kube-state-metrics and a Prometheus-compatible TSDB aren't bundled. Minimal setup:

### 1. kube-state-metrics with label allowlist

```bash
kubectl apply -f - <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata: { name: kube-state-metrics, namespace: spinup }
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: { name: kube-state-metrics }
rules:
  - apiGroups: [""]
    resources: [pods, nodes, services, namespaces, endpoints, configmaps, secrets, resourcequotas, replicationcontrollers, limitranges, persistentvolumeclaims, persistentvolumes]
    verbs: [list, watch]
  - apiGroups: [apps]
    resources: [statefulsets, daemonsets, deployments, replicasets]
    verbs: [list, watch]
  - apiGroups: [batch]
    resources: [cronjobs, jobs]
    verbs: [list, watch]
  # ... (full list at kube-state-metrics's docs)
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: { name: kube-state-metrics }
subjects: [{ kind: ServiceAccount, name: kube-state-metrics, namespace: spinup }]
roleRef: { apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kube-state-metrics }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: kube-state-metrics, namespace: spinup }
spec:
  replicas: 1
  selector: { matchLabels: { app: kube-state-metrics } }
  template:
    metadata: { labels: { app: kube-state-metrics } }
    spec:
      serviceAccountName: kube-state-metrics
      containers:
        - name: kube-state-metrics
          image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0
          args:
            - --metric-labels-allowlist=pods=[core.spinkube.dev/app-name,spinup.io/application,spinup.io/application-id]
          ports:
            - { name: http-metrics, containerPort: 8080 }
---
apiVersion: v1
kind: Service
metadata: { name: kube-state-metrics, namespace: spinup }
spec:
  selector: { app: kube-state-metrics }
  ports: [{ name: http-metrics, port: 8080, targetPort: 8080 }]
YAML
```

### 2. VictoriaMetrics single-node

```bash
kubectl apply -f - <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata: { name: victoriametrics, namespace: spinup }
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: { name: victoriametrics-scraper }
rules:
  - apiGroups: [""]
    resources: [nodes, nodes/metrics, nodes/proxy, services, endpoints, pods]
    verbs: [get, list, watch]
  - nonResourceURLs: ["/metrics", "/metrics/cadvisor"]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: { name: victoriametrics-scraper }
subjects: [{ kind: ServiceAccount, name: victoriametrics, namespace: spinup }]
roleRef: { apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: victoriametrics-scraper }
---
apiVersion: v1
kind: ConfigMap
metadata: { name: vm-scrape-config, namespace: spinup }
data:
  prometheus.yml: |
    global: { scrape_interval: 15s }
    scrape_configs:
      - job_name: otel-collector
        static_configs:
          - targets: ['spinup-otel-collector.spinup.svc.cluster.local:9464']
      - job_name: kube-state-metrics
        static_configs:
          - targets: ['kube-state-metrics.spinup.svc.cluster.local:8080']
      - job_name: kubelet-cadvisor
        scheme: https
        tls_config: { ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt, insecure_skip_verify: true }
        bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
        kubernetes_sd_configs: [{ role: node }]
        relabel_configs:
          - { target_label: __address__, replacement: kubernetes.default.svc:443 }
          - source_labels: [__meta_kubernetes_node_name]
            regex: (.+)
            target_label: __metrics_path__
            replacement: /api/v1/nodes/${1}/proxy/metrics/cadvisor
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: victoriametrics, namespace: spinup }
spec:
  replicas: 1
  selector: { matchLabels: { app: victoriametrics } }
  template:
    metadata: { labels: { app: victoriametrics } }
    spec:
      serviceAccountName: victoriametrics
      containers:
        - name: vm
          image: victoriametrics/victoria-metrics:v1.107.0
          args:
            - -promscrape.config=/etc/vm/prometheus.yml
            - -retentionPeriod=1d
            - -httpListenAddr=:8428
          ports: [{ name: http, containerPort: 8428 }]
          volumeMounts: [{ name: cfg, mountPath: /etc/vm }]
      volumes:
        - name: cfg
          configMap: { name: vm-scrape-config }
---
apiVersion: v1
kind: Service
metadata: { name: victoriametrics, namespace: spinup }
spec:
  selector: { app: victoriametrics }
  ports: [{ name: http, port: 8428, targetPort: 8428 }]
YAML
```

### 3. Point the control plane at it

```bash
# From your laptop
kubectl -n spinup port-forward svc/victoriametrics 19090:8428 &

# Restart the CP with:
SPINUP_PROMETHEUS_URL=http://localhost:19090 go run ./cmd/control-plane
```

## Sending traces to a real backend

The debug exporter dumps to stdout — useful only for confirming traces flow. For a real backend, add an `otlp` (or backend-specific) exporter to the collector's ConfigMap and route it in the traces pipeline. The chart doesn't expose this yet — patch the ConfigMap post-install.

Example addition (Jaeger over OTLP):

```yaml
exporters:
  otlp/jaeger:
    endpoint: jaeger-collector.observability.svc:4317
    tls: { insecure: true }

service:
  pipelines:
    traces:
      exporters: [debug, spanmetrics, otlp/jaeger]
```

## Own control-plane metrics

The control plane exposes its own `/metrics` on the same port as the API (default 8080). Interesting series:

- `spinup_http_requests_total{method,route,status}` — every `/api/*` request
- `spinup_http_request_duration_seconds{…}` — histogram
- `spinup_builds_finished_total{outcome}` — outcome = succeeded / failed
- `spinup_deploys_applied_total` — count of successful SpinApp Apply calls

Scrape with Prometheus / VM by pointing at the CP's Service (in-cluster) or via port-forward (local dev).

## Alerting

Not opinionated. Wire your alerting stack of choice (Alertmanager, Grafana OnCall, Pagerduty) against your Prometheus/VM. Suggested starting rules:

- `sum(rate(spinup_builds_finished_total{outcome="failed"}[5m])) > 0.1` — sustained build failures
- `sum(rate(traces_span_metrics_calls_total{http_response_status_code=~"5.."}[5m])) by (http_route) > 0` — any function with 5xx errors
- `up{job="otel-collector"} == 0` — collector down
