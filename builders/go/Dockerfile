# syntax=docker/dockerfile:1.7
# Builder image for Spinup Go functions (Spin 4).
#
# Spin 4 dropped TinyGo — Go components now use the standard Go toolchain
# (1.25+) via the `componentize-go` tool declared as a Go 1.24 tool directive.
#
# Contract:
#   IMAGE_REF env  → registry ref to push
#   /source/main.go → user source, mounted from a Secret

FROM golang:1.26-bookworm

ARG SPIN_VERSION=v4.0.2

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates git jq \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) SARCH=amd64;; arm64) SARCH=aarch64;; *) echo "unsupported arch $ARCH" >&2; exit 1;; esac; \
    curl -fsSL "https://github.com/spinframework/spin/releases/download/${SPIN_VERSION}/spin-${SPIN_VERSION}-linux-${SARCH}.tar.gz" \
        | tar -xz -C /usr/local/bin spin \
    && OTEL_SDK_DISABLED=true spin --version

# Spin's Go templates (http-go uses standard Go + componentize-go).
# OTEL_SDK_DISABLED=true works around a Spin v4.0.2 CLI bug where any
# invocation tries to init OTel and errors when no exporter is configured.
ENV OTEL_SDK_DISABLED=true
RUN spin templates install --upgrade --git https://github.com/spinframework/spin \
    && spin templates list | grep -E 'http-go|http-rust' || true

# Generate the scaffold + pre-warm module cache
WORKDIR /
RUN spin new -t http-go --accept-defaults scaffold \
    && cd scaffold && go mod tidy && go mod download

COPY entrypoint.sh /usr/local/bin/spinup-build
RUN chmod +x /usr/local/bin/spinup-build

WORKDIR /work
ENTRYPOINT ["/usr/local/bin/spinup-build"]
