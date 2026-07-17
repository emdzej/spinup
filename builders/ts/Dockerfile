# syntax=docker/dockerfile:1.7
# Builder image for Spinup TypeScript functions.
# Identical wiring to the JS builder; uses Spin's http-ts template.
FROM node:24-bookworm

ARG SPIN_VERSION=v4.0.2

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates git jq \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) SARCH=amd64;; arm64) SARCH=aarch64;; *) exit 1;; esac; \
    curl -fsSL "https://github.com/spinframework/spin/releases/download/${SPIN_VERSION}/spin-${SPIN_VERSION}-linux-${SARCH}.tar.gz" \
        | tar -xz -C /usr/local/bin spin

# OTEL_SDK_DISABLED=true works around a Spin v4.0.2 CLI bug where any
# invocation tries to init OTel and errors when no exporter is configured.
ENV OTEL_SDK_DISABLED=true
RUN spin plugins install --yes js2wasm
RUN spin templates install --upgrade --git https://github.com/spinframework/spin-js-sdk

WORKDIR /
RUN spin new -t http-ts --accept-defaults scaffold \
    && cd scaffold && npm install

# Patch the scaffold's build to minify and drop sourcemaps. Default template
# writes ~12 MB WASMs (with sourcemaps + no minify); on shim-spin-v2 the
# per-container startup budget (containerd RunContainer deadline) is ~20 s,
# and wasmtime AOT-compiling multiple 12 MB modules exceeds that — pods
# crashloop with "context deadline exceeded" before ever running our code.
# After this patch WASMs come out around 3–5 MB and start well inside the
# budget.
RUN sed -i \
    -e "s/sourcemap: true/sourcemap: false/" \
    -e "s/minify: false/minify: true/" \
    /scaffold/build.mjs

COPY entrypoint.sh /usr/local/bin/spinup-build
RUN chmod +x /usr/local/bin/spinup-build

WORKDIR /work
ENTRYPOINT ["/usr/local/bin/spinup-build"]
