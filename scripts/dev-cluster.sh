#!/usr/bin/env bash
# Run the control-plane locally against a real cluster (SpinKube, Zot,
# Keycloak, VM — all remote). Useful for tight iteration on Go/UI code
# without going through the GitHub build/deploy loop.
#
#   ./scripts/dev-cluster.sh                # kubectl-context "tve", defaults
#   KCTX=my-cluster ./scripts/dev-cluster.sh
#
# What runs on your machine:
#   - VM port-forward from monitoring/vmsingle → localhost:8428
#   - control-plane on :8080 (Go, `go run`)
#   - The UI dev server is separate — start it in another terminal with
#     `pnpm --filter ui dev` (proxies /api and /auth to localhost:8080).
#
# Everything else stays in the cluster:
#   - SpinKube reconciles the SpinApps this CP creates
#   - Build Jobs run in cluster
#   - Zot / Keycloak / cert-manager: unchanged
#
# The local CP writes to the same `spinup-functions` namespace as the
# in-cluster CP. Use distinct app names (e.g. prefix with your initials)
# to avoid clobbering someone else's work.
set -euo pipefail

KCTX=${KCTX:-tve}
FUNCTIONS_NS=${SPINUP_FUNCTIONS_NAMESPACE:-spinup-functions}
CP_NS=${SPINUP_NAMESPACE:-spinup}
MON_NS=${SPINUP_MONITORING_NS:-monitoring}
VM_LOCAL_PORT=${VM_LOCAL_PORT:-8428}
CP_ADDR=${SPINUP_HTTP_ADDR:-:8080}
DB_PATH=${SPINUP_DB_DSN:-$HOME/.local/state/spinup/dev.db}

log() { printf '\033[36m[dev]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m[dev] %s\033[0m\n' "$*" >&2; exit 1; }

# Preflight: cluster reachable + Secrets exist.
kubectl --context="$KCTX" get ns "$CP_NS" >/dev/null 2>&1 \
  || die "namespace $CP_NS missing in context $KCTX — is spinup deployed?"

log "context: $KCTX  |  functions ns: $FUNCTIONS_NS  |  CP addr: $CP_ADDR"

# Read OIDC client secret from the cluster — never echoed.
CS=$(kubectl --context="$KCTX" get secret spinup-oidc -n "$CP_NS" \
  -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d) \
  || die "couldn't read spinup-oidc/client-secret in $CP_NS"
[ -n "$CS" ] || die "spinup-oidc/client-secret is empty"
export SPINUP_OIDC_CLIENT_SECRET="$CS"

# Port-forward VM so PromQL queries from the local CP land against tve's VM.
kubectl --context="$KCTX" -n "$MON_NS" port-forward svc/vmsingle "$VM_LOCAL_PORT:8428" >/tmp/dev-vm-pf.log 2>&1 &
PF_PID=$!
cleanup() {
  log "stopping port-forward (pid $PF_PID)"
  kill "$PF_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for the port-forward to bind before starting the CP so first PromQL
# doesn't 502.
for _ in {1..20}; do
  if nc -z localhost "$VM_LOCAL_PORT" 2>/dev/null; then break; fi
  sleep 0.3
done
nc -z localhost "$VM_LOCAL_PORT" 2>/dev/null \
  || die "VM port-forward didn't come up (see /tmp/dev-vm-pf.log)"
log "VM port-forward → http://localhost:$VM_LOCAL_PORT"

mkdir -p "$(dirname "$DB_PATH")"

# --- CP env ---------------------------------------------------------------
# Match what the chart wires for the in-cluster CP; the differences are
# strictly the local overrides (redirect URL, DB path, prometheus URL).
export SPINUP_HTTP_ADDR="$CP_ADDR"
export SPINUP_KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
# Force the CP to use KCTX regardless of the kubeconfig's current-context —
# ~/.kube/config often points at rancher-desktop or kind for other work.
export SPINUP_KUBECONTEXT="$KCTX"

export SPINUP_FUNCTIONS_NAMESPACE="$FUNCTIONS_NS"
export SPINUP_FUNCTIONS_PUBLIC_DOMAIN=${SPINUP_FUNCTIONS_PUBLIC_DOMAIN:-spinup.solvely.pl}
export SPINUP_FUNCTIONS_PUBLIC_GATEWAY=${SPINUP_FUNCTIONS_PUBLIC_GATEWAY:-spinup/spinup-gateway}
export SPINUP_FUNCTIONS_IMAGE_PULL_SECRETS=${SPINUP_FUNCTIONS_IMAGE_PULL_SECRETS:-spinup-registry-creds}

# Registry — local CP creates build Jobs that push here; SpinApps pull
# through the same URL.
export SPINUP_OCI_REGISTRY_URL=${SPINUP_OCI_REGISTRY_URL:-registry.spinup.solvely.pl/spinup}
export SPINUP_OCI_AUTH_SECRET=${SPINUP_OCI_AUTH_SECRET:-spinup-registry-creds}

# Builder images — pin to whatever's currently released.
BUILDER_TAG=${SPINUP_BUILDER_TAG:-0.1.0-alpha.25}
export SPINUP_BUILDER_IMAGE_GO=${SPINUP_BUILDER_IMAGE_GO:-ghcr.io/emdzej/spinup-builder-go:$BUILDER_TAG}
export SPINUP_BUILDER_IMAGE_JS=${SPINUP_BUILDER_IMAGE_JS:-ghcr.io/emdzej/spinup-builder-js:$BUILDER_TAG}
export SPINUP_BUILDER_IMAGE_TS=${SPINUP_BUILDER_IMAGE_TS:-ghcr.io/emdzej/spinup-builder-ts:$BUILDER_TAG}
export SPINUP_BUILDER_IMAGE_RUST=${SPINUP_BUILDER_IMAGE_RUST:-ghcr.io/emdzej/spinup-builder-rust:$BUILDER_TAG}
export SPINUP_BUILDER_IMAGE_PULL_SECRETS=${SPINUP_BUILDER_IMAGE_PULL_SECRETS:-ghcr-credentials}

# Metrics — through the port-forward we just opened.
export SPINUP_PROMETHEUS_URL=${SPINUP_PROMETHEUS_URL:-http://localhost:$VM_LOCAL_PORT}

# OIDC — local redirect URL (the Keycloak client already whitelists
# http://localhost:5173/auth/callback per prior setup).
export SPINUP_OIDC_ISSUER_URL=${SPINUP_OIDC_ISSUER_URL:-https://auth.solvely.pl/realms/solvely}
export SPINUP_OIDC_CLIENT_ID=${SPINUP_OIDC_CLIENT_ID:-spinup}
export SPINUP_OIDC_REDIRECT_URL=${SPINUP_OIDC_REDIRECT_URL:-http://localhost:5173/auth/callback}
export SPINUP_AUTHZ_REQUIRED_ROLES=${SPINUP_AUTHZ_REQUIRED_ROLES:-spinup}

# Local dev DB — separate from anything else.
export SPINUP_DB_DRIVER=sqlite
export SPINUP_DB_DSN="$DB_PATH"

# Version tag so the header shows something meaningful in dev.
export SPINUP_VERSION="dev-$(git rev-parse --short HEAD 2>/dev/null || echo dev)"

log "starting control-plane at $CP_ADDR (db: $DB_PATH)"
log "UI: run 'pnpm --filter ui dev' in another terminal, then open http://localhost:5173"
cd "$(dirname "$0")/../services/control-plane"
if command -v air >/dev/null 2>&1; then
  log "air detected — hot-reload build loop"
  exec air
else
  log "air not installed — using 'go run'. Install for hot-reload:"
  log "  go install github.com/air-verse/air@latest"
  exec go run ./cmd/control-plane
fi
