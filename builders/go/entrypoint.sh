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

    # Backward-compat: the scaffold's esbuild entry point is ./src/index.{ts,js}.
    # UIs that created functions with a bare index.{ts,js} at the root would
    # otherwise be silently overridden by the scaffold's default hello-world.
    # Move the user's entry into src/ before overlaying the scaffold.
    for ext in ts tsx js mjs; do
        if [ -f "$fn_dir/index.$ext" ] && [ ! -f "$fn_dir/src/index.$ext" ]; then
            mkdir -p "$fn_dir/src"
            mv "$fn_dir/index.$ext" "$fn_dir/src/index.$ext"
            echo "  $fn_dir: moved index.$ext → src/index.$ext for scaffold layout"
        fi
    done

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

# Manifest fetch: assume HTTPS (all real registries + our exposed Zot speak it).
# Anonymous registries (ttl.sh, dev-Zot without auth) still work because we
# fall through to the unauthenticated branch below.
#
# Curl gotchas hit here before:
#  - Zot returns 405 on HEAD /v2/ so any "probe" using -I is misleading.
#  - Redirecting from http:// to https:// drops the Authorization header, so
#    HTTP-first + follow-redirects can't send auth. Just talk HTTPS directly.
manifest_url="https://${registry}/v2/${name}/manifests/${tag}"
manifest_accept='Accept: application/vnd.oci.image.manifest.v1+json'
manifest_alt='Accept: application/vnd.docker.distribution.manifest.v2+json'

if [ -f /root/.docker/config.json ]; then
  auth=$(jq -r --arg reg "$registry" '.auths[$reg].auth // empty' /root/.docker/config.json 2>/dev/null)
fi

if [ -n "${auth:-}" ]; then
  # `-u` accepts "user:password" and is properly quoted here so passwords
  # containing spaces or shell metachars survive.
  user_pass=$(printf '%s' "$auth" | base64 -d)
  size=$(curl -fsSL -u "$user_pass" -H "$manifest_accept" -H "$manifest_alt" "$manifest_url" \
    | jq '((.config.size // 0) + ([.layers[]?.size // 0] | add // 0))')
else
  size=$(curl -fsSL -H "$manifest_accept" -H "$manifest_alt" "$manifest_url" \
    | jq '((.config.size // 0) + ([.layers[]?.size // 0] | add // 0))')
fi
if [ -n "$size" ] && [ "$size" != "null" ]; then
  echo "SPINUP_IMAGE_SIZE_BYTES=$size"
else
  echo "SPINUP_IMAGE_SIZE_BYTES=unknown (manifest fetch failed)"
fi
set -e

echo "=== done ==="
