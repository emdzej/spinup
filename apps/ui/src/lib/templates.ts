import type { Language } from './types';

interface Template {
  filename: string;   // primary file the editor pre-populates
  content: string;
  buildable: boolean;
}

// Convenience: return a Files map with the primary file pre-populated.
export function initialFiles(lang: Language): Record<string, string> {
  const t = templates[lang];
  return { [t.filename]: t.content };
}

const goTemplate: Template = {
  filename: 'main.go',
  buildable: true,
  content: `package main

import (
\t"fmt"
\t"net/http"

\tspinhttp "github.com/spinframework/spin-go-sdk/v3/http"
)

func init() {
\tspinhttp.Handle(func(w http.ResponseWriter, r *http.Request) {
\t\tw.Header().Set("content-type", "text/plain")
\t\tfmt.Fprintln(w, "Hello from Spinup (Go)!")
\t})
}

func main() {}
`
};

const jsTemplate: Template = {
  // Must live at src/index.js — the builder scaffold's esbuild entry point
  // is ./src/index.{js,ts}; a bare index.{js,ts} is ignored and the scaffold's
  // default hello-world gets componentized instead (silent 404s in the app).
  filename: 'src/index.js',
  buildable: true,
  content: `// Spin uses the browser fetch-event API. The scaffold pre-installs
// itty-router for routing convenience.
import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// '*' matches any path — Spin passes the component-mount prefix through,
// so router.get('/') would only match a request at exactly the mount root.
router
  .get('*', () => new Response('Hello from Spinup (JS)!', { headers: { 'content-type': 'text/plain' } }));

addEventListener('fetch', (event) => {
  event.respondWith(router.fetch(event.request));
});
`
};

const tsTemplate: Template = {
  // Must live at src/index.ts — see jsTemplate for the reason.
  filename: 'src/index.ts',
  buildable: true,
  content: `import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// '*' matches any path — Spin passes the component-mount prefix through,
// so router.get('/') would only match a request at exactly the mount root.
router
  .get('*', () => new Response('Hello from Spinup (TS)!', { headers: { 'content-type': 'text/plain' } }));

//@ts-ignore
addEventListener('fetch', (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
`
};

const rustTemplate: Template = {
  filename: 'lib.rs',
  buildable: true,
  content: `use spin_sdk::http::{IntoResponse, Request, Response};
use spin_sdk::http_service;

#[http_service]
async fn handle_hello(_req: Request) -> anyhow::Result<impl IntoResponse> {
    Ok(Response::builder()
        .status(200)
        .header("content-type", "text/plain")
        .body("Hello from Spinup (Rust)!".to_string()))
}
`
};

export const templates: Record<Language, Template> = {
  go: goTemplate,
  js: jsTemplate,
  ts: tsTemplate,
  rust: rustTemplate
};

export function isBuildable(lang: Language): boolean {
  return templates[lang].buildable;
}
