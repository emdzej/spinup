# Core concepts

SpinUP has three primary domain objects. Understanding how they relate makes everything else in the platform obvious.

## Application

An **Application** is the deployable unit. It maps 1:1 to a Spin app, which maps 1:1 to a K8s pod.

An Application has:

- **`name`** — DNS-1123 label. Used as the SpinApp CR name, the OCI image name, the K8s Service name.
- **`language`** — one of `go`, `js`, `ts`, `rust`. Determines which builder image runs.
- **N functions** — the HTTP handlers packed into this Application.

Applications are the *packing unit*: N functions in one Application share one pod, one wasmtime process, and one OCI image.

## Function

A **Function** is one HTTP trigger inside an Application. It has:

- **`name`** — DNS-1123 label, unique within the Application.
- **`route`** — Spin route pattern. Wildcard (`/...`), prefix (`/api/...`), or exact (`/health`).
- **`source`** — a JSON blob of `{filename: content}` pairs. Edited in the Monaco editor, or uploaded as `.tar.gz`.

When you create an Application, SpinUP auto-creates a first Function with the same name. Multi-function apps add more via `POST /api/v1/applications/{id}/functions`.

::: tip Why the extra layer?
Spin apps already support multiple HTTP triggers. The reason to expose "Application" separately is so the UI can present N functions grouped under one deployable, and so the platform can reason about scheduling (one pod per Application, not per Function). This shipped as **Option A** on the [scaling roadmap](/architecture/scaling-roadmap).
:::

## Build

A **Build** is one attempt to compile + push an Application. It's a row in the `builds` table plus a K8s Batch Job that materializes it.

Lifecycle:

1. `pending` — created, Job not yet scheduled
2. `running` — Job's pod is executing `spin build` + `spin registry push`
3. `succeeded` — image pushed, `SpinApp` CR patched
4. `failed` — Job hit its backoff limit, error text captured

The build ID (a UUID without dashes) is also the OCI image tag: `registry/spinup/{app-name}:{buildId}`. That gives us immutable, content-addressable deploys.

Only successful builds produce an image. Failed builds retain their pod logs for inspection.

## How Applications run

Each Application's build produces a `SpinApp` custom resource. spin-operator translates that into a Deployment + Service. Pods use `runtimeClassName: wasmtime-spin-v2` so the [containerd-shim-spin](https://github.com/spinframework/containerd-shim-spin) runs the WASM component directly (no Node/JVM in between).

Properties:

- One pod per Application, always running (subject to `spec.replicas`, default 1)
- Standard K8s isolation — pod boundaries, ResourceQuotas, NetworkPolicies all apply
- CPU/memory limits set per-pod via `SpinApp.spec.resources`
- Horizontal auto-scaling supported via SpinKube's HPA (`SpinApp.spec.enableAutoscaling`)
- Deploy = spin-operator rolls the Deployment; old pod terminates once new pod is Ready

## Data model at a glance

```
tenants
   └── applications
         ├── language          (go | js | ts | rust)
         ├── functions[]
         │     ├── route
         │     └── source (files JSON)
         └── builds[]
               ├── status
               ├── imageRef
               └── error
```

See [Database schema](/reference/database) for the full column list.

## What's not in the model (yet)

- **Environments** (dev/staging/prod) — one control plane per cluster today.
- **Secrets** — plaintext env vars on the SpinApp CR only; a proper secrets integration is queued.
- **Non-HTTP triggers** (Redis, cron) — Spin supports them; SpinUP doesn't expose them yet.
- **Version pinning** — the latest successful build is always deployed. Manual rollback works by deleting the newer build.
- **Scale to zero + dense packing** — see the [scaling roadmap](/architecture/scaling-roadmap).
