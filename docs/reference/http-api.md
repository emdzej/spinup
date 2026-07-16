# HTTP API reference

All endpoints are served by the control plane. Base path: `/api/v1`.

Authentication: `Authorization: Bearer <id-token>` from your OIDC provider. Or, in dev with `SPINUP_DEV_INSECURE_SKIP_AUTH=true`, no auth is required.

## Applications

### List

```
GET /api/v1/applications
→ [{ id, name, language, runtime, description, createdAt }, ...]
```

### Get

```
GET /api/v1/applications/{appId}
→ { id, name, language, runtime, description, createdAt,
    functions: [{ id, name, route }, ...],
    deployment: { image, replicas, observedReplicas, ready, namespace,
                  serviceName, internalUrl, publicUrl, message } | null,
    builds: [{ id, status, imageRef, createdAt, finishedAt, error }, ...] }
```

`deployment` is `null` until the first successful build. `publicUrl` is populated only if `SPINUP_PUBLIC_BASE_URL` is set.

### Create

```
POST /api/v1/applications
{ "name": "greeter",
  "language": "go" | "js" | "ts" | "rust",
  "runtime":  "spinkube",
  "description": "optional" }
→ 201 { id, name, ..., functions: [{ id, name, route }] }
```

Auto-creates a first Function named the same as the Application, with route `/...`.

### Delete

```
DELETE /api/v1/applications/{appId}
→ 204
```

Cascades: functions, sources, builds, the SpinApp CR, and any pods/Services.

### Trigger a deploy (spinkube only)

```
POST /api/v1/applications/{appId}/deploy
→ 202 { message }
```

Re-applies the SpinApp CR with the latest successful build's image ref. Rarely needed — the build succeed handler already applies. Use if something drifted (a manual `kubectl edit` on the CR).

## Functions

### List

```
GET /api/v1/applications/{appId}/functions
→ [{ id, name, route, createdAt }, ...]
```

### Get / Create / Update / Delete

```
GET    /api/v1/applications/{appId}/functions/{fnId}
POST   /api/v1/applications/{appId}/functions          { name, route? }
PUT    /api/v1/applications/{appId}/functions/{fnId}   { route }
DELETE /api/v1/applications/{appId}/functions/{fnId}
```

`route` defaults to `/{name}/...` on Create if omitted.

## Source

### Read

```
GET /api/v1/applications/{appId}/functions/{fnId}/source
→ { files: { "main.go": "package main\n...", ... }, updatedAt }
```

### Write

```
PUT /api/v1/applications/{appId}/functions/{fnId}/source
{ "files": { "main.go": "...", "helpers.go": "..." } }
→ 200 { files, updatedAt }
```

Replaces the entire file map atomically. To add a file, PUT the merged set.

### Export

```
GET /api/v1/applications/{appId}/functions/{fnId}/source.tar.gz
→ application/gzip
```

### Import

```
POST /api/v1/applications/{appId}/functions/{fnId}/source.tar.gz
Content-Type: multipart/form-data
File field: archive
→ 200 { files, updatedAt }
```

## Builds

### List

```
GET /api/v1/applications/{appId}/builds
→ [{ id, status, imageRef, createdAt, finishedAt, error }, ...]
```

### Get

```
GET /api/v1/applications/{appId}/builds/{buildId}
→ { id, status, imageRef, createdAt, finishedAt, error }
```

### Start

```
POST /api/v1/applications/{appId}/builds
→ 202 { id, status: "pending", ... }
```

Creates a K8s Job. Poll the build until `status` is `succeeded` or `failed`.

### Stream logs

```
GET /api/v1/applications/{appId}/builds/{buildId}/logs[?follow=true]
→ text/plain streaming
```

With `follow=true`, holds the connection until the pod exits.

## Invoke

```
POST /api/v1/applications/{appId}/functions/{fnId}/invoke
{ "method": "GET",
  "path": "/hello?name=world",
  "headers": { "user-agent": ["curl/8"] },
  "body": "optional",
  "bodyIsBase64": false }
→ 200 { status, headers, body, bodyIsBase64, truncated, durationMs }
```

Response envelope always 200 — inspect `status` for the underlying HTTP code. Body base64-encoded when it's not valid UTF-8 or the content-type isn't text-ish.

Limits: 1 MiB request body, 1 MiB captured response body, 30 s timeout.

## Runtime logs

```
GET /api/v1/applications/{appId}/logs?follow=true&tail=200
→ text/plain streaming
```

Streams the pod’s stderr.

## Metrics

### Per-application (CPU + memory)

```
GET /api/v1/applications/{appId}/metrics?range=15m&step=15s
→ { range, step,
    series: { cpu:    { points: [[t, v], ...], unit: "cores" },
              memory: { points: [[t, v], ...], unit: "bytes" } } }
```

### Per-function (traffic + latency + errors)

```
GET /api/v1/applications/{appId}/functions/{fnId}/metrics?range=15m&step=15s
→ { range, step,
    series: { requestRate: { points: [[t, v], ...], unit: "req/s" },
              latencyP95:  { points: [[t, v], ...], unit: "ms" },
              errorRate:   { points: [[t, v], ...], unit: "req/s" } } }
```

`points` is an array of `[unix-seconds, value]` tuples ready for uPlot / Chart.js.

`range` accepts values like `5m`, `15m`, `1h`, `6h`, up to `24h`. `step` accepts durations `>= 1s`.

### Overview (platform-wide)

```
GET /api/v1/overview/metrics?range=15m&step=30s
→ { range, step,
    series: { httpRequestRate: { points, unit: "req/s" },
              buildRate:       { points, unit: "builds/s" } } }
```

## Errors

Uniform:

```
{ "error": "reason string" }
```

Standard HTTP codes:

- `400` — invalid input (validation failed, bad JSON)
- `401` — missing or invalid bearer token
- `403` — token valid but not authorized for the resource
- `404` — resource doesn't exist
- `409` — name conflict (Application or Function already exists)
- `413` — body too large
- `500` — internal error (also logged with slog)
- `501` — endpoint or feature not implemented for this runtime
- `503` — dependency down (SpinPost CRD missing, PromQL client not configured, worker unreachable)

## OpenAPI

Not yet — the API is small enough that this reference is the source of truth. If you want a machine-readable spec, opening an issue is welcome.
