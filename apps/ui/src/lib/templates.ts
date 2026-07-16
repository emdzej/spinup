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
  filename: 'index.js',
  buildable: true,
  content: `// Spin uses the browser fetch-event API. The scaffold pre-installs
// itty-router for routing convenience.
import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router
  .get('/', () => new Response('Hello from Spinup (JS)!', { headers: { 'content-type': 'text/plain' } }))
  .get('/hello/:name', ({ name }) => \`Hello, \${name}!\`);

addEventListener('fetch', (event) => {
  event.respondWith(router.fetch(event.request));
});
`
};

const tsTemplate: Template = {
  filename: 'index.ts',
  buildable: true,
  content: `import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router
  .get('/', () => new Response('Hello from Spinup (TS)!', { headers: { 'content-type': 'text/plain' } }))
  .get('/hello/:name', ({ name }: { name: string }) => \`Hello, \${name}!\`);

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
