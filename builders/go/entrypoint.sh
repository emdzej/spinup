#!/bin/sh
# Spinup builder entrypoint. Same script across all four language builders;
# each image differs only in what's baked into /scaffold and the toolchain.
# Tarball layout:
#   spin.toml                        — synthesized by control plane
#   functions/{name}/{user files}    — per-function user source
set -eu
: "${IMAGE_REF:?IMAGE_REF env var required}"

[ -f /source/source.tar.gz ] || { echo "error: /source/source.tar.gz missing" >&2; exit 2; }

echo "=== stage: extract user source ==="
mkdir -p /work
tar -xzf /source/source.tar.gz -C /work

echo "=== stage: overlay scaffold into each function subdir ==="
for fn_dir in /work/functions/*/; do
    [ -d "$fn_dir" ] || continue
    # User files win; skip scaffold's spin.toml (root spin.toml is synthesized).
    tar --exclude=./node_modules --exclude=./spin.toml -C /scaffold -cf - . \
        | tar --skip-old-files -C "$fn_dir" -xf -
    echo "  $fn_dir populated"
done

echo "=== stage: root spin.toml ==="
cat /work/spin.toml

echo "=== stage: spin build ==="
cd /work
spin build

echo "=== stage: spin registry push $IMAGE_REF ==="
spin registry push "$IMAGE_REF"

echo "=== stage: measure image size ==="
# Report the total on-wire size of the pushed OCI artifact so the control
# plane can display it. The build watcher greps for the SPINUP_IMAGE_SIZE_BYTES=
# line and stores the value on the build row.
set +e
registry="${IMAGE_REF%%/*}"
name_tag="${IMAGE_REF#*/}"
name="${name_tag%:*}"
tag="${name_tag##*:}"

# Pull basic auth for `$registry` from the mounted docker config, if present.
# Registries that don't require auth (older Zot mode, ttl.sh, GHCR public) still
# work — auth_arg simply stays empty.
auth_arg=""
if [ -f /root/.docker/config.json ]; then
  auth=$(jq -r --arg reg "$registry" '.auths[$reg].auth // empty' /root/.docker/config.json 2>/dev/null)
  if [ -n "$auth" ]; then
    auth_arg="-u $(echo "$auth" | base64 -d)"
  fi
fi

# HTTPS by default (all real registries speak it). Fall back to HTTP if the
# HTTPS probe fails so cluster-internal HTTP registries keep working in dev.
scheme="https"
if ! curl -fsSLI $auth_arg "https://${registry}/v2/" >/dev/null 2>&1; then
  scheme="http"
fi

size=$(curl -fsSL $auth_arg \
  -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  "${scheme}://${registry}/v2/${name}/manifests/${tag}" \
  | jq '((.config.size // 0) + ([.layers[]?.size // 0] | add // 0))')
if [ -n "$size" ] && [ "$size" != "null" ]; then
  echo "SPINUP_IMAGE_SIZE_BYTES=$size"
else
  echo "SPINUP_IMAGE_SIZE_BYTES=unknown (manifest fetch failed)"
fi
set -e

echo "=== done ==="
