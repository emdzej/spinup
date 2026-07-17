# syntax=docker/dockerfile:1.7
# Builder image for Spinup JavaScript functions.
#
# Contract:
#   IMAGE_REF env    → registry ref to push
#   /source/index.js → user source, mounted from a Secret
#
# Uses Spin's official http-js template (via `spin new -t http-js`) to
# generate a scaffold, and pre-runs `npm install` at image-build time so
# per-build cost is limited to overwriting index.js, re-running install for
# any new user deps, `spin build`, and `spin registry push`.

FROM node:24-bookworm

ARG SPIN_VERSION=v4.0.2

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates git jq \
    && rm -rf /var/lib/apt/lists/*

# Spin CLI
RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) SARCH=amd64;; arm64) SARCH=aarch64;; *) echo "unsupported arch $ARCH" >&2; exit 1;; esac; \
    curl -fsSL "https://github.com/spinframework/spin/releases/download/${SPIN_VERSION}/spin-${SPIN_VERSION}-linux-${SARCH}.tar.gz" \
        | tar -xz -C /usr/local/bin spin \
    && OTEL_SDK_DISABLED=true spin --version

# js2wasm plugin (required for Spin JS/TS builds)
# OTEL_SDK_DISABLED=true works around a Spin v4.0.2 CLI bug.
ENV OTEL_SDK_DISABLED=true
RUN spin plugins install --yes js2wasm \
    && spin plugins list

# JS/TS templates
RUN spin templates install --upgrade --git https://github.com/spinframework/spin-js-sdk \
    && spin templates list

# Generate the scaffold + pre-warm npm cache
WORKDIR /
RUN spin new -t http-js --accept-defaults scaffold \
    && cd scaffold && npm install \
    && ls -la

# Minify + drop sourcemaps in the scaffold's esbuild step. See the TS
# Dockerfile for why (containerd RunContainer deadline vs wasmtime AOT
# compile time on multi-MB WASMs).
RUN sed -i \
    -e "s/sourcemap: true/sourcemap: false/" \
    -e "s/minify: false/minify: true/" \
    /scaffold/build.mjs

COPY entrypoint.sh /usr/local/bin/spinup-build
RUN chmod +x /usr/local/bin/spinup-build

WORKDIR /work
ENTRYPOINT ["/usr/local/bin/spinup-build"]
