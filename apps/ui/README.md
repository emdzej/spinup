# ui

Svelte + TypeScript front-end for Spinup. Served as a static SPA (SvelteKit `adapter-static`).

## Dev

From repo root:

```bash
pnpm install
pnpm dev
```

Dev server runs on `http://localhost:5173` and proxies `/api/*` to the control plane at `http://localhost:8080`.

## Build

```bash
pnpm --filter ui build
```

Output goes to `apps/ui/build/` and can be served by any static file server, embedded in the control-plane binary, or shipped as a container image.
