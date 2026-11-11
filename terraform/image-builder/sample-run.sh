#!/bin/bash
# Sample run on CodeBuild (docs/builds.md "Sample runs").
#
# Runs a release's promoted image once on its fixture bundle, with no network
# at all, and keeps what it wrote as a results zip for the sample document.
# docker run, not BuildKit: a single run of a finished image, whose own
# entrypoint and command apply, with an exit status and a time limit.
#
# Isolation: --network none (nothing reachable, not even the credential
# endpoint), every capability dropped, no new privileges, a pids limit, a
# memory limit, /input read-only, only /output and /tmp writable, and no
# environment beyond INPUT_DIR, OUTPUT_DIR and the review block's parameters.
# The bundle is unpacked and the output zipped by this script, platform code;
# nothing from either is executed here.
#
# Environment (StartBuild overrides from the status lambda):
#   IMAGE           required; <registry>/<repo>@sha256:... (the promoted image)
#   BUNDLE_KEY      required; the fixture bundle's key in FIXTURES_BUCKET
#   APPLICATION_ID  required; the App Store application (the results' prefix)
#   TAG             required; the release tag
#   PARAMETERS      optional; JSON object of the review block's parameters
#   MEMORY_MB       optional; the container's memory limit (default 2048)
# From the project:
#   FIXTURES_BUCKET   bundles in, results out; empty = /tmp only (local tests)
#   RUN_TIMEOUT       seconds the container may run (default 1200)
#   MAX_OUTPUT_BYTES  cap on the results zip (default 2 GiB)
#
# Output under samples/<APPLICATION_ID>/<TAG>/ in the bucket:
#   <sha256>.zip     the app's OUTPUT_DIR as it left it
#   manifest.json    always written: status ready|failed, exit code, timing,
#                    the results' key, bytes and sha256, the log tail, error
# and one SAMPLE_RESULT line in the log.
#
# Exit status: 0 whenever a manifest was written (a failed app run is a
# result); non-zero only when this script itself failed.
set -eu

log() { echo "sample-run.sh: $*"; }
die() { echo "sample-run.sh: error: $*" >&2; exit 2; }

for v in IMAGE BUNDLE_KEY APPLICATION_ID TAG; do
  [ -n "${!v:-}" ] || die "$v is required"
done
RUN_TIMEOUT=${RUN_TIMEOUT:-1200}
MAX_OUTPUT_BYTES=${MAX_OUTPUT_BYTES:-2147483648}
MEMORY_MB=${MEMORY_MB:-2048}
PARAMETERS=${PARAMETERS:-{\}}
prefix="samples/$APPLICATION_ID/$TAG"
work=/tmp/sample-run
rm -rf "$work" && mkdir -p "$work/input" "$work/output"
chmod 777 "$work/input" "$work/output" # the image may run as any user

status=""; code=""; message=""; exit_code=""; timed_out=false
fail() { status=failed; code=$1; message=$2; log "$2"; }

# --- image ------------------------------------------------------------------
registry=${IMAGE%%/*}
case "$registry" in
  *.dkr.ecr.*.amazonaws.com)
    region=$(echo "$registry" | cut -d. -f4)
    aws ecr get-login-password --region "$region" | docker login --username AWS --password-stdin "$registry" >/dev/null 2>&1 \
      || die "could not log in to $registry"
    ;;
esac
if ! docker pull --quiet "$IMAGE" >/dev/null 2>&1; then
  fail image_unavailable "the image could not be pulled: $IMAGE"
fi

# --- bundle -----------------------------------------------------------------
if [ -z "$status" ]; then
  if [ -n "${FIXTURES_BUCKET:-}" ]; then
    aws s3 cp --only-show-errors "s3://$FIXTURES_BUCKET/$BUNDLE_KEY" "$work/bundle.tar.gz" || fail bundle_unavailable "the fixture bundle could not be read: $BUNDLE_KEY"
  else
    cp "$BUNDLE_KEY" "$work/bundle.tar.gz" || fail bundle_unavailable "the fixture bundle could not be read: $BUNDLE_KEY"
  fi
fi
if [ -z "$status" ]; then
  # GNU tar drops leading / and refuses .. members on its own.
  tar --no-same-owner --no-same-permissions -xzf "$work/bundle.tar.gz" -C "$work/input" 2>"$work/tar.err" || fail bundle_unavailable "the fixture bundle could not be unpacked: $(head -c 300 "$work/tar.err")"
  rm -rf "$work/input/.pennsieve-fixtures" # the provenance is the manifest's, not the app's input
  chmod -R a+rX "$work/input"
fi

# --- parameters -------------------------------------------------------------
# The review block's parameters as environment, names checked, values as is
# (one per line; a value with a newline is refused).
if [ -z "$status" ]; then
  if ! python3 - "$PARAMETERS" "$work/params.env" <<'PY'
import json, re, sys
raw, out = sys.argv[1], sys.argv[2]
params = json.loads(raw or "{}")
if not isinstance(params, dict):
    sys.exit("PARAMETERS must be a JSON object")
lines = []
for k, v in params.items():
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", str(k)) or k in ("INPUT_DIR", "OUTPUT_DIR", "PATH", "HOME", "LD_PRELOAD"):
        sys.exit(f"parameter name refused: {k!r}")
    v = "" if v is None else str(v)
    if "\n" in v or "\r" in v:
        sys.exit(f"parameter {k} has a newline")
    lines.append(f"{k}={v}")
with open(out, "w") as f:
    f.write("\n".join(lines) + ("\n" if lines else ""))
print(f"parameters: {', '.join(params) or 'none'}")
PY
  then
    fail invalid_parameters "the review block's parameters could not be used"
  fi
fi

# --- the run ----------------------------------------------------------------
started=$(date +%s)
if [ -z "$status" ]; then
  name="sample-$$"
  docker rm -f "$name" >/dev/null 2>&1 || true
  log "running $IMAGE with no network, ${MEMORY_MB} MB, ${RUN_TIMEOUT}s"
  if ! docker run -d --name "$name" \
      --network none --cap-drop ALL --security-opt no-new-privileges --pids-limit 4096 \
      --memory "${MEMORY_MB}m" --memory-swap "${MEMORY_MB}m" \
      --tmpfs /tmp:rw,exec,size=4g \
      -v "$work/input:/input:ro" -v "$work/output:/output" \
      -e INPUT_DIR=/input -e OUTPUT_DIR=/output --env-file "$work/params.env" \
      "$IMAGE" >/dev/null 2>"$work/run.err"; then
    fail run_failed "the container could not be started: $(head -c 300 "$work/run.err")"
  else
    while :; do
      state=$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo gone)
      [ "$state" = running ] || [ "$state" = created ] || break
      if [ $(( $(date +%s) - started )) -ge "$RUN_TIMEOUT" ]; then
        timed_out=true
        docker kill "$name" >/dev/null 2>&1 || true
        break
      fi
      sleep 5
    done
    exit_code=$(docker inspect -f '{{.State.ExitCode}}' "$name" 2>/dev/null || echo 1)
    docker logs "$name" > "$work/run.log" 2>&1 || true
    docker rm -f "$name" >/dev/null 2>&1 || true
    if $timed_out; then
      fail timed_out "the app did not finish within ${RUN_TIMEOUT}s"
    elif [ "$exit_code" != 0 ]; then
      fail run_failed "the app exited $exit_code"
    fi
  fi
fi
finished=$(date +%s)
[ -f "$work/run.log" ] || : > "$work/run.log"

# --- results ----------------------------------------------------------------
# What the app wrote is data: zipped by this script, never executed.
files=0; bytes=0; sha=""
if [ -z "$status" ]; then
  files=$(find "$work/output" -type f | wc -l | tr -d ' ')
  if [ "$files" = 0 ]; then
    fail no_outputs "the app wrote nothing to its output directory"
  else
    python3 - "$work/output" "$work/results.zip" <<'PY'
import os, sys, zipfile
root, out = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for d, _, names in os.walk(root):
        for n in sorted(names):
            p = os.path.join(d, n)
            if os.path.islink(p):
                continue
            z.write(p, os.path.relpath(p, root))
PY
    bytes=$(wc -c < "$work/results.zip" | tr -d ' ')
    sha=$(sha256sum "$work/results.zip" | cut -d' ' -f1)
    if [ "$bytes" -gt "$MAX_OUTPUT_BYTES" ]; then
      fail too_large "the results are $bytes bytes zipped; the cap is $MAX_OUTPUT_BYTES"
    else
      status=ready
    fi
  fi
fi

manifest=$work/manifest.json
python3 - "$manifest" "$prefix" "$status" "${code:-}" "${message:-}" "${exit_code:-}" "$timed_out" "$started" "$finished" "$files" "$bytes" "$sha" "$work/run.log" "$PARAMETERS" <<'PY'
import datetime, json, os, sys
out, prefix, status, code, message, exit_code, timed_out, started, finished, files, size, sha, logfile, params = sys.argv[1:]
def iso(t): return datetime.datetime.fromtimestamp(int(t), datetime.timezone.utc).isoformat()
try:
    with open(logfile, errors="replace") as f:
        tail = f.read()[-4000:]
except OSError:
    tail = ""
m = {
    "version": 1,
    "applicationId": os.environ["APPLICATION_ID"], "tag": os.environ["TAG"],
    "image": os.environ["IMAGE"], "bundle": {"key": os.environ["BUNDLE_KEY"]},
    "parameters": sorted(json.loads(params or "{}").keys()) if params else [],
    "buildId": os.environ.get("CODEBUILD_BUILD_ID", ""),
    "startedAt": iso(started), "finishedAt": iso(finished), "durationSeconds": int(finished) - int(started),
    "exitCode": int(exit_code) if exit_code not in ("", None) else None,
    "timedOut": timed_out == "true",
    "status": status, "files": int(files), "logTail": tail, "warnings": [],
}
if status == "ready":
    m["results"] = {"key": f"{prefix}/{sha}.zip", "bytes": int(size), "sha256": sha}
else:
    m["error"] = {"code": code or "internal", "message": message or "the sample run failed"}
with open(out, "w") as f:
    json.dump(m, f, indent=2)
print("SAMPLE_RESULT " + json.dumps({k: v for k, v in m.items() if k != "logTail"}))
PY

if [ -z "${FIXTURES_BUCKET:-}" ]; then
  log "FIXTURES_BUCKET is empty: results in $work/results.zip, manifest in $manifest (status $status)"
  exit 0
fi
if [ "$status" = ready ]; then
  aws s3 cp --only-show-errors "$work/results.zip" "s3://$FIXTURES_BUCKET/$prefix/$sha.zip" || die "uploading the results failed"
  log "uploaded s3://$FIXTURES_BUCKET/$prefix/$sha.zip ($bytes bytes)"
fi
aws s3 cp --only-show-errors --content-type application/json "$manifest" "s3://$FIXTURES_BUCKET/$prefix/manifest.json" || die "uploading the manifest failed"
log "manifest at s3://$FIXTURES_BUCKET/$prefix/manifest.json (status $status)"
