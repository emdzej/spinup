# Introduction

SpinUP is a small platform that lets you deploy [Spin](https://spinframework.dev) HTTP functions to a Kubernetes cluster without touching kubectl. You write source in the browser (or upload a tarball), the platform builds a WebAssembly component, pushes it to an OCI registry, and creates a running Spin app.

If you've used Vercel, Netlify Functions, or Google Cloud Functions, the user-facing model is similar. The runtime underneath is [wasmtime](https://wasmtime.dev) via [SpinKube](https://www.spinkube.dev).

## What SpinUP gives you

- **Multi-language HTTP functions**: Go, JavaScript, TypeScript, Rust.
- **A UI**: SvelteKit + Monaco editor. Create an app, edit source, hit Build & Deploy, invoke it, watch logs and metrics — no `kubectl` required.
- **A control plane**: Go HTTP API. Manages Applications and Functions, synthesizes multi-component `spin.toml`, runs builds as Kubernetes Jobs, applies `SpinApp` CRs, proxies invocations, streams pod logs.
- **A builder pipeline**: pre-baked per-language Docker images that run `spin build` + `spin registry push` inside a Kubernetes Job on demand.
- **Observability**: OpenTelemetry traces from the shim, a spanmetrics connector for per-function RED metrics, per-pod CPU/memory from cAdvisor.

## What SpinUP is not

- **Not a general PaaS.** Functions are Spin HTTP components. Long-running services, TCP servers, and non-HTTP triggers are out of scope.
- **Not a Spin replacement.** It uses Spin's builder + shim + SDK unchanged; it just automates the K8s glue.
- **Not multi-region.** One control plane per cluster today. Multi-cluster is a future project.
- **Not "true" serverless yet.** Every Application is an always-on pod. Scale-to-zero is on the [scaling roadmap](/architecture/scaling-roadmap).

## When to use SpinUP

- Your team wants to ship small HTTP services and doesn't want to write Deployments.
- You already run Kubernetes and want to keep the ops story consistent.
- You like Spin's sub-millisecond WASM instantiation and want that developer flow without hand-rolling SpinKube manifests.

## When to skip SpinUP

- You need long-running processes, background workers, or non-HTTP triggers.
- Your functions need heavy native dependencies (SQL client libraries, ML runtimes) — WASM support is improving but still narrower than a JVM or Node runtime.
- You want a hosted, managed serverless offering — SpinUP is self-hosted.

## What you'll need

See [Requirements](/install/requirements) for the full list. In short:

- A Kubernetes cluster (Rancher Desktop k3s works great for local dev)
- The [containerd-shim-spin](https://github.com/spinframework/containerd-shim-spin) shim installed on nodes
- [cert-manager](https://cert-manager.io) and [spin-operator](https://github.com/spinframework/spin-operator)
- An OCI registry (or use the Zot deployment the chart can install)
- An OIDC provider (or dev-skip for local use)

## Next steps

- [Quick start](/guide/quick-start) — get a function running in ~10 minutes.
- [Core concepts](/guide/concepts) — Applications, Functions, Builds.
- [Architecture](/architecture/overview) — how the pieces fit together.
