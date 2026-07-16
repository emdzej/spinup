# Writing functions

Each language has a pre-baked template that ships with a working HTTP handler. Edit it in the Monaco editor on the function detail page, or push files via the API.

## Go

```go
package main

import (
    "fmt"
    "net/http"

    spinhttp "github.com/spinframework/spin-go-sdk/v3/http"
)

func init() {
    spinhttp.Handle(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("content-type", "text/plain")
        fmt.Fprintln(w, "Hello from SpinUP (Go)!")
    })
}

func main() {}
```

Toolchain: Go 1.26 + `componentize-go` v0.3.3 (compiled ahead of time into the builder image).

You can add more files (`helpers.go`, packages under subdirectories, a custom `go.mod`) — all files you save in the editor end up in the tar the builder Job unpacks. Extra Go modules `go mod tidy` fetches at build time.

## JavaScript

```js
import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router
  .get('/', () => new Response('Hello from SpinUP (JS)!', { headers: { 'content-type': 'text/plain' } }))
  .get('/hello/:name', ({ name }) => `Hello, ${name}!`);

addEventListener('fetch', (event) => {
  event.respondWith(router.fetch(event.request));
});
```

Toolchain: Node 24 + the `js2wasm` Spin plugin. Uses the Spin `fetch-event` API. `itty-router` ships pre-installed; add your own deps via `package.json`.

## TypeScript

```typescript
import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router
  .get('/', () => new Response('Hello from SpinUP (TS)!', { headers: { 'content-type': 'text/plain' } }))
  .get('/hello/:name', ({ name }: { name: string }) => `Hello, ${name}!`);

// @ts-ignore
addEventListener('fetch', (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
```

Same runtime as JS; the builder compiles TS before invoking `js2wasm`.

## Rust

```rust
use spin_sdk::http::{IntoResponse, Request, Response};
use spin_sdk::http_service;

#[http_service]
async fn handle_hello(_req: Request) -> anyhow::Result<impl IntoResponse> {
    Ok(Response::builder()
        .status(200)
        .header("content-type", "text/plain")
        .body("Hello from SpinUP (Rust)!".to_string()))
}
```

Toolchain: Rust 1.97 + `wasm32-wasip2` target + Spin SDK 6. The builder scaffold contains a warm Cargo registry, so first-build times are dominated by user-code compilation, not registry hydration.

## Adding files

Click the **+ file** tab in the editor to add a new file. The tab shows the filename; click the `×` to remove it (you can't remove the last file).

The set of files in the editor is the set of files the builder Job sees. Anything not in the editor is not in the build.

## Multi-file layouts by language

- **Go**: any `.go` file at the module root, plus a `go.mod`. Subdirectories work if you configure them in `go.mod` as packages.
- **JS**: `index.js` is the entrypoint. Add helper `.js` files and `import` them. Add deps to `package.json` and the builder will `npm install` before compiling.
- **TS**: `index.ts` is the entrypoint. Same story as JS with TypeScript compilation on top.
- **Rust**: `lib.rs` is the entrypoint. Add `Cargo.toml` deps or additional `.rs` files. The scaffold sets crate name to `scaffold`; don't change that (the CP synthesizes `source = "target/wasm32-wasip2/release/scaffold.wasm"`).

## Import / export

Every function page has **Import .tar.gz** and **Export .tar.gz** buttons. Export gives you the exact set of files the builder would see. Import replaces the file set atomically.

```bash
# Export
curl "http://localhost:8080/api/v1/applications/$APP/functions/$FN/source.tar.gz" -o fn.tgz

# Import (multipart form; a curl example lives in the reference doc)
curl -X POST "http://localhost:8080/api/v1/applications/$APP/functions/$FN/source.tar.gz" \
  -F 'archive=@fn.tgz'
```

## Where routes come from

Every Function has a `route` field. The default is `/...` (wildcard — catches everything). Change it on the function page (click **edit** next to the route pill). New routes take effect on the **next build** — the current pod keeps serving the old routing table until it's replaced.

Multi-function apps use disjoint route prefixes. Example:

- `greeter`: `route = "/hello/..."`
- `farewell`: `route = "/bye/..."`

Spin's internal router matches the longest prefix, so `/hello/world` goes to `greeter` and `/bye/soon` goes to `farewell`.

## SDK docs

SpinUP uses Spin's SDKs unchanged. For full API references:

- **Go**: <https://pkg.go.dev/github.com/spinframework/spin-go-sdk/v3>
- **JS/TS**: <https://spinframework.dev/v3/javascript-components>
- **Rust**: <https://docs.rs/spin-sdk>

Language pages under <https://spinframework.dev/v3/> cover key/value, SQLite, Redis, Postgres, LLM, and outbound HTTP components — all usable from SpinUP functions as long as you configure `allowed_outbound_hosts` in the source (Spin's rule) and the target hosts are reachable from your cluster.
