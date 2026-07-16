# Creating applications

An Application is the deployable unit in SpinUP. Every function lives inside one.

## From the UI

1. Open the UI (default `http://localhost:5173`).
2. Click **New application**.
3. Fill in:
   - **Name**: DNS-1123 compliant (lowercase alphanumerics + hyphens, starts + ends with a letter or digit). This becomes the SpinApp name, the OCI image name, and the K8s Service name.
   - **Language**: Go, JavaScript, TypeScript, or Rust. Determines which builder image runs.
   - **Description** (optional): shows in the applications list.
4. Click **Create**.

SpinUP auto-creates a first Function with the same name as the Application, and pre-populates its `main.go` / `index.js` / `index.ts` / `lib.rs` with the language template. You land on the function detail page ready to edit.

## From the API

```bash
curl -X POST http://localhost:8080/api/v1/applications \
  -H 'content-type: application/json' \
  -d '{
    "name": "greeter",
    "language": "go",
    "description": "Says hi in five ways"
  }'
```

Response includes the new Application ID and its auto-created first Function:

```json
{
  "id": "…",
  "name": "greeter",
  "language": "go",
  "functions": [
    { "id": "…", "name": "greeter", "route": "/..." }
  ]
}
```

## Adding more functions

An Application can host multiple functions, each with its own HTTP route. All functions in an Application share:

- One Spin runtime process (one pod)
- One OCI image per build
- One `spin.toml` (with N `[[trigger.http]]` blocks and N `[component.X]` blocks)

Add a function via the **+ Add function** button on the application page, or:

```bash
curl -X POST "http://localhost:8080/api/v1/applications/$APP_ID/functions" \
  -H 'content-type: application/json' \
  -d '{"name": "farewell", "route": "/bye/..."}'
```

The route defaults to `/{function-name}/...` if you omit it.

## Deleting

Deleting an Application removes:

- The `applications` row (cascades to `functions`, `sources`, `builds`)
- The SpinApp CR
- Function pods, Services, and any Ingress/Gateway resources tied to the SpinApp

It **doesn't** delete:

- The pushed OCI images in the registry — garbage-collect those separately
- Historical metric time series in your TSDB — they age out via retention policy

```bash
curl -X DELETE http://localhost:8080/api/v1/applications/$APP_ID
```

## What you can change vs what you can't

| Property | Editable after create? |
|---|---|
| `name` | No — used as the SpinApp CR name |
| `language` | No — locks the builder |
| `description` | Yes |
| Function `route` | Yes — takes effect on next build |
| Function `source` | Yes — takes effect on next build |
| Function count | Yes — add or remove functions |

Everything on the right is metadata; the left column would require recreating the Application.
