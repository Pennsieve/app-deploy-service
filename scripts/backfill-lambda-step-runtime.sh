#!/bin/sh
# Backfill: give App Store images built before the Lambda step runtime its
# layer and label, in place (docs/builds.md, "The Lambda step runtime").
#
# For each image tag given (one per line on stdin, <registry>/<repo>:<tag>):
#   1. skip it when it already carries the label;
#   2. export the runtime image's filesystem (one file) as a layer tarball;
#   3. delete the tag's image and its <tag>-soci index (the repository's tags
#      are immutable; the SOCI index builder re-indexes the new push);
#   4. push the image with the layer appended and the label set, to the same
#      tag, with crane mutate.
# Compute nodes must not be updated to a release that runs Lambda steps
# through the runtime until this has run in the environment.
#
# Usage:
#   RUNTIME_IMAGE=932986170183.dkr.ecr.us-east-1.amazonaws.com/pennsieve/lambda-step-runtime:v1.33.0 \
#   aws ecr describe-images --repository-name dev-appstore-private-use1 \
#     --query 'imageDetails[].imageTags[]' --output text | tr '\t' '\n' | grep -v -- '-soci$' \
#     | sed 's#^#<account>.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1:#' \
#     | sh scripts/backfill-lambda-step-runtime.sh
#
# Needs crane (go install github.com/google/go-containerregistry/cmd/crane@latest),
# the aws CLI, and credentials that can read and push appstore-private.
# DRY_RUN=1 lists what would change. The list of tags to backfill is the
# versions whose computeTypes include lambda; every tag is safe, since one
# that already has the label is skipped.
set -eu

: "${RUNTIME_IMAGE:?set RUNTIME_IMAGE to the runtime layer image}"
label=net.pennsieve.lambda-step-runtime
version=${RUNTIME_VERSION:-${RUNTIME_IMAGE##*:}}
dry=${DRY_RUN:-}

log() { echo "backfill: $*"; }

login() { # <registry>
  region=$(echo "$1" | cut -d. -f4)
  aws ecr get-login-password --region "$region" | crane auth login "$1" -u AWS --password-stdin >/dev/null
}

login "${RUNTIME_IMAGE%%/*}"
layer=$(mktemp -t lambda-step-runtime.XXXXXX)
crane export "$RUNTIME_IMAGE" "$layer"
log "runtime layer from $RUNTIME_IMAGE ($(wc -c <"$layer" | tr -d ' ') bytes)"

logged=" "
while IFS= read -r image; do
  [ -n "$image" ] || continue
  registry=${image%%/*}
  case "$logged" in *" $registry "*) ;; *) login "$registry"; logged="$logged$registry " ;; esac
  if crane config "$image" 2>/dev/null | grep -q "\"$label\""; then
    log "$image: already has the runtime"
    continue
  fi
  if [ -n "$dry" ]; then
    log "$image: would append the runtime"
    continue
  fi
  repo_and_tag=${image#*/}
  repo=${repo_and_tag%:*}
  tag=${repo_and_tag##*:}
  region=$(echo "$registry" | cut -d. -f4)
  # the old image and its SOCI index, so the tags can be pushed again
  aws ecr batch-delete-image --region "$region" --repository-name "$repo" \
    --image-ids imageTag="$tag" imageTag="$tag-soci" >/dev/null
  crane mutate "$image" --append "$layer" --label "$label=$version" --tag "$image" >/dev/null
  log "$image: runtime $version appended"
done
rm -f "$layer"
