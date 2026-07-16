# builder/go

Builder image for Spinup Go functions. Consumed by the control plane as a
Kubernetes Job.

## Build

```bash
cd builders/go
docker build -t spinup/builder-go:latest .

# Load into a local kind cluster so the Job can use it without pushing to
# a registry:
kind load docker-image spinup/builder-go:latest --name spinup
```

## Contract

The control plane creates a Job with:

- Env `IMAGE_REF` — target OCI ref (e.g. `ttl.sh/spinup/hello:<build-id>`)
- Volume `/source` (Secret) containing `main.go`

The container copies the scaffold (`go.mod`, `spin.toml`, base `main.go`) into
`/work`, overlays the user's `main.go`, runs `go mod tidy`, `spin build`, and
`spin registry push --insecure $IMAGE_REF`. On exit code 0 the control plane
applies the SpinApp CR with the new image ref.

## What's baked in

- **Spin CLI** — for `spin build` and `spin registry push`
- **TinyGo** — the scaffold's `spin.toml` compiles with `tinygo build -target=wasi`
- **Go module cache** — pre-fetched by running `go mod download` on the scaffold, so per-build `go mod tidy` on user imports only pulls new deps

## User source contract (V1)

Users provide a single `main.go` that imports `github.com/fermyon/spin/sdk/go/v2/http` and calls `spinhttp.Handle(...)` in `init()`. Multi-file support (via Monaco editor) is a follow-up.
