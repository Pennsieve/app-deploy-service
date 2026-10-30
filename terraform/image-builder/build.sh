#!/bin/sh
# App Store image build: BuildKit on CodeBuild (docs/builds.md).
#
# Builds a Dockerfile straight from git and pushes the image, the way the
# kaniko task does, with BuildKit (the engine behind `docker build`). The
# CodeBuild project runs this from its buildspec; it also runs as is in the
# rootful moby/buildkit image for local tests.
#
# Environment (set per build by the provisioner, StartBuild overrides):
#   CONTEXT          required; git context, e.g.
#                    https://github.com/org/repo.git#refs/tags/v1.2.3
#                    (git:// is rewritten to https://, as kaniko did)
#   DESTINATION      required; image to push, <registry>/<repo>:<tag>
#   BUILD_CACHE_REF  optional; this app's registry cache, <registry>/<repo>:<key>.
#                    Imported before and exported (mode=max) after the build.
#   BUILD_CACHE_IMPORT_REF
#                    optional; another cache to import (never exported): the
#                    app's default variant's, for a build of another variant
#   BUILD_ARGS       optional; Docker build args, one NAME=value per line
#                    (app.yml build.args, a variant's merged over the top level's)
#   BUILD_TARGET     optional; the Dockerfile stage to build (default: the last)
#   DOCKERFILE       optional; the Dockerfile's path in the repository
#                    (default: Dockerfile)
#   VARIANT          optional; the app variant this build makes, for the log
#   GIT_TOKEN        optional; token for a private GitHub repo
#   BUILD_PLATFORM   optional; default linux/amd64 (what the runners run)
#   DOCKERHUB_USERNAME, DOCKERHUB_PASSWORD
#                    optional; authenticated Docker Hub pulls
#   DOCKERHUB_MIRROR optional; registry mirror for docker.io, <registry>/<path>,
#                    e.g. the ECR pull-through cache <account>.dkr.ecr.<region>.amazonaws.com/docker-hub.
#                    buildkitd tries it first and falls back to Docker Hub.
#   BUILDKIT_HOST    optional; use this buildkitd instead of starting one
#   BUILDKITD_FLAGS  optional; extra flags for the buildkitd started here
#   DOCKER_CONFIG    optional; where registry credentials are written
#                    (default $HOME/.docker)
#
# ECR registries named by DESTINATION, BUILD_CACHE_REF or DOCKERHUB_MIRROR are logged in to
# with `aws ecr get-login-password` (the CodeBuild service role).
#
# Exit status is buildctl's: 0 = pushed, anything else = failed.
set -eu

log() { echo "build.sh: $*"; }
die() { echo "build.sh: error: $*" >&2; exit 2; }

[ -n "${CONTEXT:-}" ] || die "CONTEXT is required"
[ -n "${DESTINATION:-}" ] || die "DESTINATION is required"
platform=${BUILD_PLATFORM:-linux/amd64}

# GitHub turned off git://; kaniko silently cloned over https.
case "$CONTEXT" in
  git://*) CONTEXT="https://${CONTEXT#git://}" ;;
esac
# BuildKit clones an https context only when the URL ends in .git; otherwise
# it downloads the URL as a plain file (GitHub's HTML page).
url=${CONTEXT%%#*}
fragment=${CONTEXT#"$url"}
case "$url" in
  *.git) ;;
  *) url="${url%/}.git" ;;
esac
CONTEXT="$url$fragment"

# --- registry credentials -------------------------------------------------
# buildctl reads the docker config on the client side and hands credentials
# to buildkitd over its session, so RUN steps never see them.
DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}
export DOCKER_CONFIG
mkdir -p "$DOCKER_CONFIG"

auths=""
add_auth() { # <registry> <user> <password>
  encoded=$(printf '%s:%s' "$2" "$3" | base64 | tr -d '\n')
  auths="${auths:+$auths,}\"$1\":{\"auth\":\"$encoded\"}"
}

ecr_logged_in=" "
for ref in "$DESTINATION" "${BUILD_CACHE_REF:-}" "${BUILD_CACHE_IMPORT_REF:-}" "${DOCKERHUB_MIRROR:-}"; do
  [ -n "$ref" ] || continue
  registry=${ref%%/*}
  case "$registry" in
    *.dkr.ecr.*.amazonaws.com) ;;
    *) continue ;;
  esac
  case "$ecr_logged_in" in
    *" $registry "*) continue ;;
  esac
  region=$(echo "$registry" | cut -d. -f4)
  password=$(aws ecr get-login-password --region "$region")
  add_auth "$registry" AWS "$password"
  ecr_logged_in="$ecr_logged_in$registry "
  log "logged in to $registry"
done

if [ -n "${DOCKERHUB_USERNAME:-}" ] && [ -n "${DOCKERHUB_PASSWORD:-}" ]; then
  add_auth "https://index.docker.io/v1/" "$DOCKERHUB_USERNAME" "$DOCKERHUB_PASSWORD"
  log "using Docker Hub credentials for pulls"
fi

(umask 077 && printf '{"auths":{%s}}\n' "$auths" > "$DOCKER_CONFIG/config.json")

# --- buildkitd ------------------------------------------------------------
# Rootful, with the process sandbox on (the default): RUN steps get their own
# PID and mount namespaces and cannot see buildkitd, buildctl or their
# credentials. CodeBuild runs the project privileged so buildkitd can do this.
if [ -z "${BUILDKIT_HOST:-}" ]; then
  # buildkitd clones git contexts with the git binary.
  command -v git >/dev/null 2>&1 || die "git is required to build from a git context"

  # The overlayfs snapshotter cannot sit on an overlay filesystem (a
  # container's root); buildkitd then falls back to "native", which copies
  # the whole filesystem for every step. Keep its state on the first
  # candidate directory that is not overlay (CodeBuild's build volume).
  root=""
  for dir in ${BUILDKIT_ROOT_CANDIDATES:-/codebuild/output /var/lib/docker}; do
    [ -d "$dir" ] || continue
    fstype=$(stat -f -c %T "$dir" 2>/dev/null || echo unknown)
    case "$fstype" in
      overlay* | unknown) continue ;;
    esac
    root="$dir/buildkit"
    log "buildkitd state in $root ($fstype)"
    break
  done

  # Base images from Docker Hub go through the mirror first; buildkitd
  # falls back to registry-1.docker.io when the mirror fails.
  config=""
  if [ -n "${DOCKERHUB_MIRROR:-}" ]; then
    config=/tmp/buildkitd.toml
    printf '[registry."docker.io"]\n  mirrors = ["%s"]\n' "$DOCKERHUB_MIRROR" > "$config"
    log "docker.io mirror: $DOCKERHUB_MIRROR"
  fi

  BUILDKIT_HOST=unix:///run/buildkit/buildkitd.sock
  export BUILDKIT_HOST
  mkdir -p /run/buildkit
  # shellcheck disable=SC2086 # BUILDKITD_FLAGS is a list of flags
  buildkitd --addr "$BUILDKIT_HOST" ${root:+--root "$root"} ${config:+--config "$config"} ${BUILDKITD_FLAGS:-} >/tmp/buildkitd.log 2>&1 &
  tries=0
  until buildctl debug workers >/dev/null 2>&1; do
    tries=$((tries + 1))
    if [ "$tries" -ge 60 ]; then
      cat /tmp/buildkitd.log >&2
      die "buildkitd did not start"
    fi
    sleep 1
  done
fi
log "buildkit: $(buildctl --version)"
buildctl debug workers -v 2>/dev/null | grep -i -E 'snapshotter|platforms' | head -3 || true

# --- build ----------------------------------------------------------------
# No attestations: the pushed tag must stay one image manifest (not an index
# with an attestation manifest), the shape the SOCI index builder and
# image_resolver expect. buildctl adds none unless asked, and ANY
# --opt attest:provenance=... value asks, even "false" or "disabled=true"
# (both checked with v0.34.1: they push an index with an attestation
# manifest). So provenance stays off by never passing attest:*.
# Layers are gzip, which SOCI can index.
set -- build \
  --frontend dockerfile.v0 \
  --opt "context=$CONTEXT" \
  --opt "platform=$platform" \
  --output "type=image,name=$DESTINATION,push=true,compression=gzip" \
  --metadata-file /tmp/build-metadata.json \
  --progress plain

if [ -n "${GIT_TOKEN:-}" ]; then
  # The git source authenticates github.com with this secret.
  set -- "$@" --secret id=GIT_AUTH_TOKEN,env=GIT_TOKEN
fi

# app.yml's build settings (dockerfile.v0 frontend options).
if [ -n "${BUILD_ARGS:-}" ]; then
  # one NAME=value per line; names were checked by the provisioner, and a
  # value may hold spaces, "=" or quotes (each is one argv entry)
  while IFS= read -r arg; do
    [ -n "$arg" ] || continue
    case "$arg" in
      [A-Za-z_]*=*) ;;
      *) die "BUILD_ARGS line is not NAME=value: $arg" ;;
    esac
    set -- "$@" --opt "build-arg:$arg"
    log "build arg ${arg%%=*}"
  done <<EOF
$BUILD_ARGS
EOF
fi
if [ -n "${BUILD_TARGET:-}" ]; then
  set -- "$@" --opt "target=$BUILD_TARGET"
fi
if [ -n "${DOCKERFILE:-}" ]; then
  # read from the git context, like the default Dockerfile
  set -- "$@" --opt "filename=$DOCKERFILE"
fi

if [ -n "${BUILD_CACHE_REF:-}" ]; then
  # One cache per app (per variant: a variant other than the default has
  # its own, so concurrent builds of one release never export to the same
  # ref); a failed export never fails the build.
  set -- "$@" --import-cache "type=registry,ref=$BUILD_CACHE_REF"
  if [ -n "${BUILD_CACHE_IMPORT_REF:-}" ] && [ "$BUILD_CACHE_IMPORT_REF" != "$BUILD_CACHE_REF" ]; then
    # layers shared with the default variant (base image, dependencies)
    set -- "$@" --import-cache "type=registry,ref=$BUILD_CACHE_IMPORT_REF"
  fi
  set -- "$@" \
    --export-cache "type=registry,ref=$BUILD_CACHE_REF,mode=max,image-manifest=true,oci-mediatypes=true,ignore-error=true"
fi

log "building $CONTEXT${VARIANT:+ (variant $VARIANT)}${BUILD_TARGET:+ target $BUILD_TARGET}${DOCKERFILE:+ from $DOCKERFILE} for $platform -> $DESTINATION"
start=$(date +%s)
buildctl "$@"
end=$(date +%s)

digest=$(sed -n 's/.*"containerimage.digest": *"\([^"]*\)".*/\1/p' /tmp/build-metadata.json | head -1)
media_type=$(tr -d '\n' </tmp/build-metadata.json | sed -n 's/.*"containerimage.descriptor": *{[^}]*"mediaType": *"\([^"]*\)".*/\1/p')
# Same shape as kaniko's last line ("Pushed <repo>@sha256:..."), for log searches.
echo "Pushed ${DESTINATION%:*}@${digest} (${media_type:-unknown media type}) in $((end - start))s"

case "$media_type" in
  *index*|*manifest.list*)
    # Should not happen (see above). Fail loudly rather than mark a version
    # deployed whose tag the SOCI index builder cannot index.
    die "pushed an image index ($media_type), expected a single image manifest"
    ;;
esac
