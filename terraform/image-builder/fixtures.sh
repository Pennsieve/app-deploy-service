#!/bin/sh
# Fixture bundle build: BuildKit on CodeBuild (docs/builds.md "Fixture bundles").
#
# Builds the test inputs an App Store app declares in its app.yml review
# block, once per release, into a bundle that sample runs unpack instead of
# fetching anything. The fixture logic is the platform's own app-fixtures
# image (pennsieve-apps/app-fixtures); it runs as a Dockerfile RUN step, so
# the generators and dataset fetches it executes, which are the app author's
# code, get the same sandbox as an image build: own network namespace on the
# bridge, the credential and metadata endpoints dropped, the DNS Firewall
# allow-list, 80 and 443 only. This script, platform code, only tars what the
# step exported and uploads it.
#
# Environment (StartBuild overrides from the status lambda):
#   REPOSITORY        required; the app's repository URL
#   TAG               required; the release tag
#   APPLICATION_ID    required; the App Store application id (the bundle's prefix)
#   PORTS             optional; comma-separated input ports (empty: every port
#                     the review block covers)
# From the CodeBuild project:
#   APP_FIXTURES_IMAGE  required; <registry>/<repo>:<tag> of app-fixtures
#   FIXTURES_BUCKET     the bundle bucket; empty = write to /tmp only (local tests)
#   MAX_BUNDLE_BYTES    hard cap on the compressed bundle (default 2 GiB)
#   WARN_BUNDLE_BYTES   warning above this (default 200 MiB)
#   MAX_DATASET_BYTES   cap on one declared dataset, passed to app-fixtures
#   GENERATE_TIMEOUT    seconds a generator may run, passed to app-fixtures
#   BUILD_PLATFORM      optional; default linux/amd64 (what the runners run)
#   BUILDKIT_HOST, BUILDKITD_FLAGS, RUN_NETWORK, DOCKER_CONFIG as in build.sh
#
# Output in the bucket, under fixtures/<APPLICATION_ID>/<TAG>/:
#   <sha256>.tar.gz   the bundle: the target's INPUT_DIR layout plus
#                     .pennsieve-fixtures/ (provenance crate or error.json)
#   manifest.json     always written: status ready|failed, the bundle's key,
#                     bytes and sha256, warnings, the error code and message
# and one FIXTURES_RESULT line in the log with the manifest.
#
# Exit status: 0 whenever a manifest was written, including a failed fixture
# build (the app's declaration is the author's to fix; the manifest says
# what); non-zero only when this script itself failed (buildkit, upload).
set -eu

log() { echo "fixtures.sh: $*"; }
die() { echo "fixtures.sh: error: $*" >&2; exit 2; }

for v in REPOSITORY TAG APPLICATION_ID APP_FIXTURES_IMAGE; do
  eval "val=\${$v:-}"
  [ -n "$val" ] || die "$v is required"
done
MAX_BUNDLE_BYTES=${MAX_BUNDLE_BYTES:-2147483648}
WARN_BUNDLE_BYTES=${WARN_BUNDLE_BYTES:-209715200}
MAX_DATASET_BYTES=${MAX_DATASET_BYTES:-2147483648}
GENERATE_TIMEOUT=${GENERATE_TIMEOUT:-1200}
platform=${BUILD_PLATFORM:-linux/amd64}
prefix="fixtures/$APPLICATION_ID/$TAG"

# --- registry credentials and buildkitd (buildkitd.sh) --------------------
# shellcheck source=buildkitd.sh
. "${BUILDKIT_LIB:-$(dirname "$0")/buildkitd.sh}"
registry_login "$APP_FIXTURES_IMAGE"
start_buildkitd

# --- the build ------------------------------------------------------------
# A two-stage Dockerfile: app-fixtures writes /out, and a scratch stage holds
# nothing but /out, so the local export is exactly the bundle. The RUN step
# keeps app-fixtures' exit status in .pennsieve-fixtures/exit instead of
# failing the build: a declaration problem is a result, not a build error.
# Values reach the step as build args: never interpolated into the command.
work=/tmp/fixtures-build
rm -rf "$work" && mkdir -p "$work/dockerfile" "$work/context"
cat > "$work/dockerfile/Dockerfile" <<'DF'
ARG APP_FIXTURES_IMAGE
FROM ${APP_FIXTURES_IMAGE} AS fixtures
ARG REPOSITORY
ARG TAG
ARG PORTS=
ARG MAX_DATASET_BYTES
ARG GENERATE_TIMEOUT
ENV REPOSITORY=${REPOSITORY} TAG=${TAG} PORTS=${PORTS} \
    MAX_DATASET_BYTES=${MAX_DATASET_BYTES} GENERATE_TIMEOUT=${GENERATE_TIMEOUT} \
    OUTPUT_DIR=/out
RUN mkdir -p /out/.pennsieve-fixtures && python3 -m processor.main; echo $? > /out/.pennsieve-fixtures/exit

FROM scratch
COPY --from=fixtures /out /
DF

bundle_dir=$work/bundle
log "building fixtures for $REPOSITORY at $TAG (ports: ${PORTS:-all}) with $APP_FIXTURES_IMAGE"
start=$(date +%s)
# --no-cache: the generators must run every time; nothing of this build is
# worth keeping, and a cached step would hide a changed review block.
buildctl build \
  --frontend dockerfile.v0 \
  --local "context=$work/context" \
  --local "dockerfile=$work/dockerfile" \
  --opt "platform=$platform" \
  --opt "build-arg:APP_FIXTURES_IMAGE=$APP_FIXTURES_IMAGE" \
  --opt "build-arg:REPOSITORY=$REPOSITORY" \
  --opt "build-arg:TAG=$TAG" \
  --opt "build-arg:PORTS=${PORTS:-}" \
  --opt "build-arg:MAX_DATASET_BYTES=$MAX_DATASET_BYTES" \
  --opt "build-arg:GENERATE_TIMEOUT=$GENERATE_TIMEOUT" \
  --no-cache \
  --output "type=local,dest=$bundle_dir" \
  --progress plain
end=$(date +%s)
log "fixture step finished in $((end - start))s"

# --- the bundle -----------------------------------------------------------
# From here on everything under $bundle_dir is the step's output: data,
# never executed, never put on a command line.
[ -d "$bundle_dir/.pennsieve-fixtures" ] || die "the fixture step exported nothing"
tarball=$work/bundle.tar.gz
tar -C "$bundle_dir" -czf "$tarball" .
bytes=$(wc -c < "$tarball" | tr -d ' ')
sha=$(sha256sum "$tarball" | cut -d' ' -f1)
files=$(find "$bundle_dir" -type f ! -path "$bundle_dir/.pennsieve-fixtures/*" | wc -l | tr -d ' ')

# The manifest: what the lambda records on the version.
manifest=$work/manifest.json
python3 - "$bundle_dir" "$manifest" "$prefix" "$bytes" "$sha" "$files" "$MAX_BUNDLE_BYTES" "$WARN_BUNDLE_BYTES" "$start" "$end" <<'PY'
import json, os, sys, datetime
bundle, out, prefix, size, sha, files, hard, warn, start, end = sys.argv[1:]
size, hard, warn, files = int(size), int(hard), int(warn), int(files)
meta = os.path.join(bundle, ".pennsieve-fixtures")
def read(name):
    try:
        with open(os.path.join(meta, name)) as f:
            return f.read()
    except OSError:
        return None
exit_code = (read("exit") or "").strip()
error = None
raw = read("error.json")
if raw:
    try:
        e = json.loads(raw)
        error = {"code": str(e.get("code") or "internal"), "message": str(e.get("message") or "")[:2000]}
    except ValueError:
        error = {"code": "internal", "message": "app-fixtures wrote an unreadable error.json"}
elif exit_code not in ("0", ""):
    error = {"code": "internal", "message": f"app-fixtures exited {exit_code} without an error.json"}
elif exit_code == "":
    error = {"code": "internal", "message": "the fixture step left no exit status"}
crate = None
raw = read("ro-crate-metadata.json")
if raw:
    try:
        crate = json.loads(raw)
    except ValueError:
        crate = None
warnings = []
manifest = {
    "version": 1,
    "applicationId": os.environ["APPLICATION_ID"],
    "tag": os.environ["TAG"],
    "repository": os.environ["REPOSITORY"],
    "ports": [p for p in os.environ.get("PORTS", "").split(",") if p],
    "appFixturesImage": os.environ["APP_FIXTURES_IMAGE"],
    "buildId": os.environ.get("CODEBUILD_BUILD_ID", ""),
    "startedAt": datetime.datetime.fromtimestamp(int(start), datetime.timezone.utc).isoformat(),
    "builtAt": datetime.datetime.fromtimestamp(int(end), datetime.timezone.utc).isoformat(),
    "files": files,
}
if error:
    manifest["status"] = "failed"
    manifest["error"] = error
elif size > hard:
    manifest["status"] = "failed"
    manifest["error"] = {"code": "too_large", "message": f"the bundle is {size} bytes compressed; the cap is {hard}"}
else:
    if size > warn:
        warnings.append(f"large_bundle: {size} bytes compressed, above {warn}")
    manifest["status"] = "ready"
    manifest["bundle"] = {"key": f"{prefix}/{sha}.tar.gz", "bytes": size, "sha256": sha}
manifest["warnings"] = warnings
if crate is not None:
    manifest["provenance"] = crate
with open(out, "w") as f:
    json.dump(manifest, f, indent=2)
print("FIXTURES_RESULT " + json.dumps({k: v for k, v in manifest.items() if k != "provenance"}))
PY

status=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["status"])' "$manifest")
if [ -z "${FIXTURES_BUCKET:-}" ]; then
  log "FIXTURES_BUCKET is empty: bundle in $tarball ($bytes bytes), manifest in $manifest (status $status)"
  exit 0
fi
if [ "$status" = ready ]; then
  aws s3 cp --only-show-errors "$tarball" "s3://$FIXTURES_BUCKET/$prefix/$sha.tar.gz" || die "uploading the bundle failed"
  log "uploaded s3://$FIXTURES_BUCKET/$prefix/$sha.tar.gz ($bytes bytes)"
fi
aws s3 cp --only-show-errors --content-type application/json "$manifest" "s3://$FIXTURES_BUCKET/$prefix/manifest.json" || die "uploading the manifest failed"
log "manifest at s3://$FIXTURES_BUCKET/$prefix/manifest.json (status $status)"
