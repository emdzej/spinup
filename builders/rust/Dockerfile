# syntax=docker/dockerfile:1.7
# Builder image for Spinup Rust functions.
#
# Rust builds are slow — even with a warm cargo registry cache, a wasm32 build
# takes ~30–60s. A persistent sccache PVC would help but is a follow-up.

FROM rust:1.97-bookworm

ARG SPIN_VERSION=v4.0.2

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates git jq \
    && rm -rf /var/lib/apt/lists/*

# Spin CLI
RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) SARCH=amd64;; arm64) SARCH=aarch64;; *) exit 1;; esac; \
    curl -fsSL "https://github.com/spinframework/spin/releases/download/${SPIN_VERSION}/spin-${SPIN_VERSION}-linux-${SARCH}.tar.gz" \
        | tar -xz -C /usr/local/bin spin \
    && OTEL_SDK_DISABLED=true spin --version

# Spin 4 (SDK 6) targets WASI Preview 2. Keep wasip1 too for legacy templates.
RUN rustup target add wasm32-wasip1 wasm32-wasip2

# OTEL_SDK_DISABLED=true works around a Spin v4.0.2 CLI bug.
ENV OTEL_SDK_DISABLED=true
# Spin's Rust templates + pre-fetched cargo registry
RUN spin templates install --upgrade --git https://github.com/spinframework/spin

WORKDIR /
RUN spin new -t http-rust --accept-defaults scaffold \
    && cd scaffold && cargo fetch

COPY entrypoint.sh /usr/local/bin/spinup-build
RUN chmod +x /usr/local/bin/spinup-build

WORKDIR /work
ENTRYPOINT ["/usr/local/bin/spinup-build"]
