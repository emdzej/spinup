# SpinUP documentation

VitePress site. Content lives as `.md` files under this directory; theme + nav in `.vitepress/config.ts`.

## Local dev

```bash
pnpm --filter spinup-docs dev
# open http://localhost:5173  (or whatever port VitePress picks)
```

## Build

```bash
pnpm --filter spinup-docs build
# static output → docs/.vitepress/dist
```

## Structure

- `index.md` — landing page (hero + features grid)
- `guide/` — introduction, quick start, core concepts
- `install/` — requirements, local dev, production Helm install
- `user-guide/` — creating apps, writing functions, building, invoking, logs & metrics
- `architecture/` — overview, control plane, builders, runtimes, observability, scaling roadmap
- `reference/` — HTTP API, control-plane env vars, chart values, database schema

The canonical scaling roadmap lives at `docs/scaling-roadmap.md` at the repo root (usable outside VitePress); `architecture/scaling-roadmap.md` links to it.

## Editing

Standard Markdown + [VitePress extensions](https://vitepress.dev/guide/markdown). Common ones we use:

- Frontmatter for landing page hero/features (`index.md`)
- Custom containers: `::: tip`, `::: warning`, `::: danger`
- Section anchors: `## Some heading {#custom-id}` → link with `[link](/page#custom-id)`

Search is client-side (VitePress's built-in local search provider) — no external service to configure.

## Publishing

The `.vitepress/dist` output is a static site. Serve from any static host (Netlify, Vercel, Pages, S3+CloudFront). Or bundle into the control-plane image and serve at `/docs`.
