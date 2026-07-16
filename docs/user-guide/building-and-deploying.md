# Building & deploying

A **Build** is one attempt to compile the Application's functions into a single OCI image and push it to the registry. Builds run as short-lived Kubernetes Jobs; you don't provision anything to make them happen.

## Triggering a build

- **UI**: on the application page, click **Build & Deploy**. Watch the row that appears in the Builds table; click it to see the streaming logs.
- **API**: `POST /api/v1/applications/{appId}/builds`. The response is the new build's ID and initial status (`pending`).

Only one build runs at a time per Application. Concurrent triggers are queued (the control plane serializes them via the watcher goroutine per Application).

## Build lifecycle

```
pending  ─►  running  ─►  succeeded
                   │
                   ▼
                failed
```

- **pending**: The control plane has created a `builds` row and a K8s Batch Job, but the Job's pod hasn't started yet.
- **running**: The pod is executing `spin build` and then `spin registry push`.
- **succeeded**: Image pushed to the registry with tag `{buildId}`. Control plane immediately applies the SpinApp CR with the new `image` field, and the K8s Deployment rolls forward.
- **failed**: The pod hit its `backoffLimit` (default 0 — one attempt). The error text (extracted from the log) is stored on the build row.

## What each stage does

### 1. Sourcing the tar

The control plane packs every function's `{filename: content}` map into an in-memory tar tree:

```
functions/
  greeter/
    main.go
    go.mod
  farewell/
    main.go
    go.mod
spin.toml         (synthesized: one [component.X] and one [[trigger.http]] per function)
```

That tar is stored as a K8s Secret (`src-{buildId}`) in the `spinup-functions` namespace, mounted into the build Job's pod at `/source/source.tar.gz`.

### 2. Running the Job

The Job spec uses the language-appropriate builder image:

- `spinup/builder-go:latest` for `language: go`
- `spinup/builder-js:latest`, `spinup/builder-ts:latest`, `spinup/builder-rust:latest` for the others

Env vars:

- `IMAGE_REF=registry.…/spinup/{appName}:{buildId}` — the target OCI ref
- Any `oci.auth.existingSecret` mounted at `/root/.docker/config.json`

The builder entrypoint:

1. Overlays the user's tar on top of the pre-baked Spin scaffold
2. Runs `spin build` (each `[component.X.build]` command executes in its `workdir`)
3. Runs `spin registry push $IMAGE_REF --insecure`
4. Exits 0 (or non-zero on failure)

Failed steps leave the pod in `Error` state; the control plane records the stderr and the last few stdout lines as the build error message.

### 3. Applying the SpinApp

On success, the control plane calls its `spinapp.Client.Apply(...)` (server-side apply) with the new image ref. Under `runtime: spinkube`, this creates or updates a `SpinApp` CR in `spinup-functions`. spin-operator sees the change and rolls the Deployment.
## Rebuild behavior

Every build is independent:

- New tag (`{buildId}`) — immutable, content-addressable
- New push — the registry keeps both old and new; retention is up to your registry
- Rolling deploy — one pod at a time is replaced (Kubernetes default)

You can trigger a rebuild without changing source (useful for pinning a fresh build ID, or if a builder image was updated). Just click **Build & Deploy** again.

## Rolling back

There's no dedicated rollback endpoint. To roll back:

1. Find the older build ID you want (from the Builds table on the application page)
2. Delete the newer builds via `DELETE /api/v1/applications/{appId}/builds/{buildId}` — future work; not implemented in the CP yet
3. Or apply the SpinApp directly with an older image ref via `kubectl edit spinapp {name} -n spinup-functions`

::: warning
Manual `kubectl edit` on the SpinApp CR bypasses the control plane's view of state. The next successful build will overwrite your manual change. Prefer rebuilding from an older source (import an older `.tar.gz` and rebuild) if the rollback needs to survive.
:::

## Build failures

Common ones and where to look:

| Error | Where | Fix |
|---|---|---|
| `Job has reached the specified backoff limit` | build row `error` | Look at the build log (UI) — usually a compile error in user code |
| `no such tool "componentize-go"` | build log | Go builder image is stale — rebuild it with `nerdctl build …` |
| `failed to pull and unpack image "registry…"` | pod events | Registry unreachable — check containerd mirror config (see [Local dev](/install/local-dev#containerd-mirror)) |
| `pull access denied` | pod events | Builder image not in containerd — `nerdctl --namespace k8s.io images` |
| `no runtime for "spin" is configured` | pod events | Shim missing or broken — see [Shim panic](/install/local-dev#rancher-shim-panic) |
| `apply spinapp: … field not declared` | control-plane log | Chart out of sync with spin-operator's CRD version — update spin-operator |

## Concurrency and limits

- **One build per Application at a time.** The control plane's build-watcher goroutine serializes them.
- **No global limit.** Ten Applications building simultaneously = ten pods running. Cluster CPU/RAM is the natural bound.
- **Job pod resources** aren't tuned yet — builders can request as much CPU as the node has. For production, set `containers[].resources` on the builder pods via a mutating webhook or by baking limits into the Dockerfile.
- **Build history retention** — every build row and Job persist forever unless you `DELETE` them. On a busy cluster, add a cleanup CronJob.

## Persisting build artefacts

The tar Secret (`src-{buildId}`) is kept after the Job completes so you can re-download the source that produced a specific image. The Job itself is kept for its log-retention window (default: as long as the K8s garbage collector allows). If your cluster policies delete completed Jobs aggressively, wrap builds in a longer-lived controller — future work.
