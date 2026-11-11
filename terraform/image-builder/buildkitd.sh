#!/bin/sh
# Shared by build.sh (App Store images) and fixtures.sh (fixture bundles):
# registry credentials for buildctl, and a buildkitd whose RUN steps are
# isolated (docs/builds.md "Isolation"). Sourced; the caller defines log
# and die and is running with set -eu.
#
# registry_login <refs...>   writes $DOCKER_CONFIG/config.json with an ECR
#                            login for every ECR registry among the refs
#                            (aws ecr get-login-password, the service role)
#                            and the Docker Hub credentials when set
# start_buildkitd            starts buildkitd unless BUILDKIT_HOST is set,
#                            with RUN steps on the bridge network and the
#                            credential endpoints dropped; sets
#                            started_buildkitd=1 when it started one

registry_login() {
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
  for ref in "$@"; do
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
}

start_buildkitd() {
  # Rootful, with the process sandbox on (the default): RUN steps get their own
  # PID and mount namespaces and cannot see buildkitd, buildctl or their
  # credentials. CodeBuild runs the project privileged so buildkitd can do this.
  #
  # RUN steps also get their own network namespace (--oci-worker-net=bridge:
  # BuildKit's own bridge, buildkit0 on 10.10.0.0/16, with the CNI plugins
  # from the release tarball). Without it BuildKit's "auto" mode falls back to
  # the build container's network, where a RUN step can reach CodeBuild's
  # container-credentials endpoint (169.254.170.2): a probe on 2026-11-11
  # showed it answering. The step still has no credentials URI, but a network
  # boundary should not depend on that secret. Forwarded traffic from the
  # bridge to the link-local credential and metadata endpoints is dropped
  # before buildkitd starts; everything else (package indexes, code hosts)
  # stays reachable. RUN_NETWORK=host turns this off for a diagnosis.
  started_buildkitd=""
  if [ -z "${BUILDKIT_HOST:-}" ]; then
    # buildkitd clones git contexts with the git binary.
    command -v git >/dev/null 2>&1 || die "git is required to build from a git context"
    started_buildkitd=1

    run_network=${RUN_NETWORK:-bridge}
    if [ "$run_network" = bridge ]; then
      command -v iptables >/dev/null 2>&1 || die "iptables is required for bridge networking of RUN steps"
      # A FORWARD drop alone is not enough: the credentials endpoint is a
      # DNAT in nat PREROUTING (169.254.170.2:80 to the agent's local port),
      # which rewrites the destination before FORWARD sees the packet. Probe
      # v0.2.4 on 2026-11-11 still got HTTP 404/400 from it through the bridge.
      # raw PREROUTING runs before nat, so the drop goes there (mangle when the
      # raw table is missing); FORWARD keeps the plain case. The build
      # container's own traffic leaves through OUTPUT and is untouched.
      pre_table=raw
      iptables -t raw -L PREROUTING -n >/dev/null 2>&1 || pre_table=mangle
      for addr in 169.254.170.2/32 169.254.169.254/32; do
        # Insert once; a retried phase must not stack rules.
        iptables -t "$pre_table" -C PREROUTING -d "$addr" -j DROP 2>/dev/null || iptables -t "$pre_table" -I PREROUTING 1 -d "$addr" -j DROP
        iptables -C FORWARD -d "$addr" -j DROP 2>/dev/null || iptables -I FORWARD 1 -d "$addr" -j DROP
      done
      log "RUN steps: bridge network; 169.254.170.2 and 169.254.169.254 dropped in $pre_table PREROUTING and FORWARD"
      BUILDKITD_FLAGS="--oci-worker-net=bridge ${BUILDKITD_FLAGS:-}"
    else
      log "RUN steps: ${run_network} network (RUN_NETWORK override)"
      BUILDKITD_FLAGS="--oci-worker-net=${run_network} ${BUILDKITD_FLAGS:-}"
    fi

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
  buildctl debug workers -v 2>/dev/null | grep -i -E 'snapshotter|platforms|network' | head -4 || true
  # The worker must report its RUN network as cni (what bridge mode uses);
  # "auto" silently falls back to host, which is what the isolation above
  # prevents. Label: org.mobyproject.buildkit.worker.network.
  if [ -n "$started_buildkitd" ] && [ "${RUN_NETWORK:-bridge}" = bridge ]; then
    if ! buildctl debug workers -v 2>/dev/null | grep -q -E 'worker\.network:[[:space:]]+(cni|bridge)$'; then
      cat /tmp/buildkitd.log >&2
      die "buildkitd is not using bridge networking for RUN steps"
    fi
  fi
}
