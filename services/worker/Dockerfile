# syntax=docker/dockerfile:1.7
# Multi-stage build for spinup-worker.
# Stage 1 compiles the binary with cargo, stage 2 ships the runtime + spin CLI.

FROM rust:1.97-bookworm AS builder

WORKDIR /src
COPY Cargo.toml ./
COPY src ./src

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/src/target \
    cargo build --release && \
    cp /src/target/release/spinup-worker /out-spinup-worker

FROM debian:bookworm-slim AS runtime

ARG SPIN_VERSION=v3.6.3

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# Spin CLI is needed for `spin registry pull` at runtime.
RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) SARCH=amd64;; arm64) SARCH=aarch64;; *) exit 1;; esac; \
    curl -fsSL "https://github.com/spinframework/spin/releases/download/${SPIN_VERSION}/spin-${SPIN_VERSION}-linux-${SARCH}.tar.gz" \
        | tar -xz -C /usr/local/bin spin

COPY --from=builder /out-spinup-worker /usr/local/bin/spinup-worker

RUN useradd -r -u 10001 -g nogroup -m -d /var/lib/spinup-worker spinup
USER 10001
WORKDIR /var/lib/spinup-worker

EXPOSE 8000
ENV SPINUP_WORKER_ADDR=0.0.0.0:8000
ENV SPINUP_CACHE_DIR=/var/lib/spinup-worker
ENTRYPOINT ["/usr/local/bin/spinup-worker"]
