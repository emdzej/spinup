<!--
The canonical version of this doc lives at docs/scaling-roadmap.md at the
repo root so it's usable outside VitePress builds too. Both point at the
same content; keep them in sync until we consolidate.
-->

<script setup>
import ScalingRoadmap from '../scaling-roadmap.md';
</script>

# Scaling roadmap

The canonical text of the scaling roadmap lives at [`docs/scaling-roadmap.md`](https://github.com/emdzej/spinup/blob/main/docs/scaling-roadmap.md) at the repo root.

Quick recap:

- **Option A — Component packing (shipped)**: Applications are the first-class packing unit above Functions. One SpinApp / one pod / N components sharing a wasmtime process.
- **Option B — KEDA scale-to-zero (queued)**: opt-in per Application, HTTP scaler wakes pods on request.
- **Option C — Worker pool (blocked upstream)**: shared wasmtime host in one Deployment, N apps loaded on demand from OCI. Code complete; blocked on Spin ↔ wasmtime WASI HTTP RC version drift.

For the full analysis, tradeoffs, and effort estimates, read [scaling-roadmap.md](https://github.com/emdzej/spinup/blob/main/docs/scaling-roadmap.md).
