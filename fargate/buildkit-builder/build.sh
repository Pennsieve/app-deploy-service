#!/bin/sh
# BuildKit drop-in for the kaniko executor (SPIKE PROTOTYPE, see docs/buildkit-spike.md).
#
# Accepts the arguments app-provisioner passes to kaniko today, so main.go and the
# status lambda do not change:
#
#   build.sh --context git://github.com/org/repo.git#refs/tags/v1.2.3 \
#            --destination <account>.dkr.ecr.<region>.amazonaws.com/<repo>:<tag> [--force]
#
# Environment:
#   GIT_TOKEN          optional; token for private GitHub repos (as with kaniko)
#   BUILD_CACHE_REF    optional; registry cache ref (e.g. <ecr>/<cache-repo>:<app-hash>).
#                      Imported before and exported (mode=max) after the build.
#   BUILD_PLATFORM     optional; default linux/amd64 (what Fargate runs)
#   BUILDKITD_FLAGS    passed to buildkitd by buildctl-daemonless.sh
#   AWS_*              credentials for docker-credential-ecr-login (env or task role)
#
# Exit status is buildctl's: 0 = pushed, non-zero = failed. The status lambda keys
# off the container exit code, exactly as with kaniko.
set -eu

context=""
destination=""
while [ $# -gt 0 ]; do
  case "$1" in
    --context) context="$2"; shift 2 ;;
    --context=*) context="${1#*=}"; shift ;;
    --destination) destination="$2"; shift 2 ;;
    --destination=*) destination="${1#*=}"; shift ;;
    --force) shift ;; # kaniko-only (skip container detection); nothing to do
    *) echo "build.sh: unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ -n "$context" ] || { echo "build.sh: --context is required" >&2; exit 2; }
[ -n "$destination" ] || { echo "build.sh: --destination is required" >&2; exit 2; }

# kaniko's git context is "git://host/path#refs/tags/x" but clones over https;
# GitHub turned off the git:// protocol, so BuildKit must be given https.
case "$context" in
  git://*) context="https://${context#git://}" ;;
esac

set -- build \
  --frontend dockerfile.v0 \
  --opt "context=$context" \
  --opt "platform=${BUILD_PLATFORM:-linux/amd64}" \
  --output "type=image,name=$destination,push=true" \
  --metadata-file /tmp/build-metadata.json \
  --progress plain
# No attestations: buildctl adds none unless asked (no --opt attest:*), so the
# pushed tag is one image manifest, the shape kaniko pushes and the SOCI index
# builder + image_resolver expect.

if [ -n "${GIT_TOKEN:-}" ]; then
  # The dockerfile frontend authenticates git contexts with this secret.
  set -- "$@" --secret id=GIT_AUTH_TOKEN,env=GIT_TOKEN
fi

if [ -n "${BUILD_CACHE_REF:-}" ]; then
  set -- "$@" \
    --import-cache "type=registry,ref=$BUILD_CACHE_REF" \
    --export-cache "type=registry,ref=$BUILD_CACHE_REF,mode=max,image-manifest=true,oci-mediatypes=true,ignore-error=true"
fi

echo "build.sh: building $context -> $destination"
start=$(date +%s)
buildctl-daemonless.sh "$@"
end=$(date +%s)

digest=$(sed -n 's/.*"containerimage.digest": *"\([^"]*\)".*/\1/p' /tmp/build-metadata.json | head -1)
# Same shape as kaniko's last line ("Pushed <repo>@sha256:..."), handy for log searches.
echo "Pushed ${destination%:*}@${digest} in $((end - start))s"
