#!/bin/sh
# Backfill: give App Store images built before the Lambda step runtime its
# layer and label, in place (docs/builds.md, "The Lambda step runtime").
#
# For each image tag given (one per line on stdin, <registry>/<repo>:<tag>):
#   1. skip it when it already carries the label;
#   2. push the image with the runtime layer appended and the label set,
#      under a temporary tag <tag>-rt (crane mutate; the layers are already
#      in the repository, so only the new layer and the manifest go up);
#   3. delete the old tag and its <tag>-soci index (the repository's tags
#      are immutable; the SOCI index builder re-indexes the new push);
#   4. tag the new image <tag> and drop <tag>-rt.
# The old image is deleted only once the new one is in the repository: ECR
# deletes an image with its last tag, so a push that reads the old image
# after its tag is gone would lose it. The SOCI index builder may also index
# the temporary tag; those <tag>-rt-soci indexes are removed at the end, and
# any made after that (the builder runs a little behind) by running the
# script again or deleting them by hand.
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
# Needs crane (brew install crane, or go install
# github.com/google/go-containerregistry/cmd/crane@latest), the aws CLI, and
# credentials that can read and push appstore-private and pull the runtime
# image. DRY_RUN=1 lists what would change. The list of tags to backfill is
# the versions whose computeTypes include lambda (the appstore versions
# table, manifest.runtime.computeTypes); every tag is safe, since one that
# already has the label is skipped.
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

# delete_tags <registry> <repo> <tag>... deletes the tags that exist.
delete_tags() {
  _registry=$1; _repo=$2; shift 2
  _region=$(echo "$_registry" | cut -d. -f4)
  _ids=""
  for _t in "$@"; do _ids="$_ids imageTag=$_t"; done
  # shellcheck disable=SC2086
  aws ecr batch-delete-image --region "$_region" --repository-name "$_repo" \
    --image-ids $_ids --query 'failures[?failureCode!=`ImageNotFound`]' --output text
}

login "${RUNTIME_IMAGE%%/*}"
layer=$(mktemp -t lambda-step-runtime.XXXXXX)
crane export "$RUNTIME_IMAGE" "$layer"
log "runtime layer from $RUNTIME_IMAGE ($(wc -c <"$layer" | tr -d ' ') bytes)"

logged=" "
repos=" "
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
  case "$repos" in *" $registry/$repo "*) ;; *) repos="$repos$registry/$repo " ;; esac
  temp="$image-rt"
  crane mutate "$image" --append "$layer" --label "$label=$version" --tag "$temp" >/dev/null
  delete_tags "$registry" "$repo" "$tag" "$tag-soci"
  crane tag "$temp" "$tag" >/dev/null
  delete_tags "$registry" "$repo" "$tag-rt"
  log "$image: runtime $version appended"
done
rm -f "$layer"

# Indexes the SOCI index builder made for the temporary tags.
for r in $repos; do
  registry=${r%%/*}; repo=${r#*/}
  region=$(echo "$registry" | cut -d. -f4)
  stray=$(aws ecr describe-images --region "$region" --repository-name "$repo" \
    --query 'imageDetails[].imageTags[]' --output text | tr '\t' '\n' | grep -- '-rt-soci$' || true)
  for t in $stray; do
    delete_tags "$registry" "$repo" "$t"
    log "$r:$t: stray index removed"
  done
done
