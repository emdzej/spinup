# syntax=docker/dockerfile:1.7
# Multi-arch build via `docker buildx --platform linux/amd64,linux/arm64 …`.

# --------------------------------------------------------------------------
# UI stage — build the SvelteKit static output that gets embedded in the CP.
# Runs on the build platform (Node needs no cross-compile).
# --------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:24-bookworm AS ui

RUN corepack enable

WORKDIR /src

# Lockfile-first for cache locality. Copy manifests, install, then source.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/ui/package.json apps/ui/
COPY docs/package.json docs/
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --filter ui

COPY apps/ui apps/ui
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm --filter ui build

# --------------------------------------------------------------------------
# Go stage — compile the control plane. The UI stage's output is dropped into
# services/control-plane/internal/webui/dist/ so //go:embed picks it up.
# --------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY services/control-plane/go.mod services/control-plane/go.sum ./services/control-plane/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    cd services/control-plane && go mod download

COPY services/control-plane ./services/control-plane

# Bake the UI into the embed directory. Overwrites the .gitkeep placeholder.
COPY --from=ui /src/apps/ui/build ./services/control-plane/internal/webui/dist

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    cd services/control-plane \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
       go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane

# --------------------------------------------------------------------------
# Runtime — distroless, no shell, non-root, minimal CVE surface.
# --------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/control-plane /control-plane

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/control-plane"]
