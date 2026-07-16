---
layout: home

hero:
  name: SpinUP
  text: Cloud-functions for Spin, on Kubernetes
  tagline: Build and deploy WebAssembly HTTP functions with a Monaco editor and one click. Runs on any Kubernetes cluster with SpinKube.
  actions:
    - theme: brand
      text: Quick start
      link: /guide/quick-start
    - theme: alt
      text: Architecture
      link: /architecture/overview

features:
  - title: Applications, not just functions
    details: Group related functions into an Application. One Spin runtime, one pod, N HTTP triggers — sub-millisecond per-request instantiation.
  - title: Multi-language builders
    details: Go, JavaScript, TypeScript, and Rust. Each builder is a container image that runs as a Kubernetes Job on demand.
  - title: OpenTelemetry-native observability
    details: The SpinAppExecutor forwards traces to an in-cluster OTel Collector. A spanmetrics connector derives per-function RED metrics; the UI plots them from the control plane.
  - title: SSO (OIDC) ready
    details: Bring any OIDC provider. No local users; identity flows from your IdP to every /api/* call.
---
