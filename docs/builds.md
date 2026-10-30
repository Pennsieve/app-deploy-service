# App Store image builds

When a release is published to the App Store, this service builds the
release's root `Dockerfile` from git at the release tag and pushes the image to
`<env>-appstore-private-<region>` as `<hash>-<tag>`. The SOCI index builder
then adds `<hash>-<tag>-soci`, and `image_resolver` hands out the SOCI-enabled
digest.

Two builders can do the build. The Terraform variable `image_builder` picks one
per environment:

| `image_builder` | What builds | Notes |
|---|---|---|
| `kaniko` (default) | the kaniko Fargate task (`gcr.io/kaniko-project/executor`, v1.24.0 pinned by digest) | kaniko is archived. It ignores Dockerfile heredocs and `RUN --mount`, so releases using them are refused (below). There is no cache. |
| `codebuild` | BuildKit v0.34.1 in a CodeBuild project (`terraform/codebuild.tf`) | The same engine as `docker build`, with a per-app registry cache. |

Background: the spike in [buildkit-spike.md](buildkit-spike.md). It explains why
BuildKit cannot run on Fargate and why CodeBuild was chosen.

## Flow

```
release ─► service lambda (post_appstore_handler) ─► provisioner task (ADD_TO_APPSTORE)
                │ kaniko only: refuse BuildKit-only Dockerfiles      │
                │                                                    ├─ kaniko:    ecs.RunTask (deployer task)
                │                                                    └─ codebuild: codebuild.StartBuild
                ▼
        version "registering" ─► "deploying" ─► status lambda ─► "deployed" | "error"
                                                   ▲
              ECS Task State Change (kaniko) ──────┤
              CodeBuild Build State Change ────────┘
```

Both paths have the same inputs: the source URL, the tag, the destination
`<appstore-private>:<hash>-<tag>`, a GitHub token for a private repo, and the
storage a GPU build needs (`APP_STORAGE` from app.yml). Both have the same
output, the image pushed to that tag. The deployment record and the version
status go through the same steps.

### CodeBuild path

- **Provisioner** (`fargate/app-provisioner/provisioner/imagebuild`) calls
  `StartBuild` on `<env>-app-deploy-service-image-builder-<region>` and
  overrides these environment variables:
  - `CONTEXT`: `https://github.com/<org>/<repo>.git#refs/tags/<tag>`;
  - `DESTINATION`;
  - `BUILD_CACHE_REF`;
  - `DEPLOYMENT_ID`, `APPLICATION_ID` (the version) and `APPLICATIONS_TABLE`
    (the versions table).

  The deployment id is the idempotency token.
- **Private repos:** the provisioner writes the token to an SSM SecureString,
  `/<env>/app-deploy-service/image-builds/<deploymentId>/git-token`. The build
  reads it as `GIT_TOKEN` through CodeBuild's `PARAMETER_STORE` variable type,
  so the token never appears among the build's recorded variables. BuildKit
  gets it as the secret `GIT_AUTH_TOKEN`. The status lambda deletes the
  parameter when the build ends.
- **Build** (`terraform/image-builder/build.sh`, inlined into the buildspec):
  - the install phase downloads the pinned BuildKit release tarball and checks
    its sha256;
  - build.sh logs in to ECR with `aws ecr get-login-password`;
  - it starts a rootful `buildkitd` with the process sandbox on;
  - it runs:

  ```
  buildctl build --frontend dockerfile.v0 \
    --opt context=<git url#tag> --opt platform=linux/amd64 \
    --output type=image,name=<destination>,push=true,compression=gzip \
    [--secret id=GIT_AUTH_TOKEN,env=GIT_TOKEN] \
    --import-cache type=registry,ref=<cache> \
    --export-cache type=registry,ref=<cache>,mode=max,image-manifest=true,oci-mediatypes=true,ignore-error=true
  ```

  The pushed tag is one OCI image manifest
  (`application/vnd.oci.image.manifest.v1+json`, OCI config, gzip layers): no
  index and no attestation manifest. The SOCI index builder's V2 validation
  accepts this (`ValidateImageDigest`), and ECS and Fargate pull OCI images.

  Provenance is off because no `attest:*` option is passed. With buildctl
  v0.34.1, `--opt attest:provenance=false` and `=disabled=true` both turn
  provenance **on** and push an index (checked). As a safeguard, build.sh fails
  the build if it ever pushes an index.
- **Status:** an EventBridge rule sends the project's `CodeBuild Build State
  Change` events to the status lambda (`lambda/status/handler/codebuild.go`).
  It reads the ids from the build's environment variables, or from
  `BatchGetBuilds` when the event lacks them. It records the build like an ECS
  task:

  | build-status | deployment `lastStatus` | `errored` | version |
  |---|---|---|---|
  | IN_PROGRESS | RUNNING | | (stays deploying) |
  | SUCCEEDED | STOPPED, `stopCode` SUCCEEDED | false | deployed |
  | FAILED, FAULT, STOPPED, TIMED_OUT | STOPPED, `stopCode` = the status | true | error |

  The deployment's `taskArn` holds the build ARN. On a failure,
  `stoppedReason` holds:
  - the failed phase and its context;
  - the last 25 lines the build printed, without CodeBuild's own
    `[Container]` lines;
  - a link to the full log.

  It is capped at 3,500 characters.

### Cache

- One cache per app: the tag `app-<sha256(sourceUrl)[:40]>` in
  `<env>-appstore-buildcache-<region>`. Apps never share a cache, so one app's
  Dockerfile cannot seed layers that another app reuses. ECR cannot scope IAM
  to a tag, so the isolation comes from the build inputs. A RUN step cannot
  reach the builder's credentials (below).
- The cache lives in its own repository, not in appstore-private. The SOCI
  index builder only watches appstore-private, and cache manifests would
  otherwise mix with app tags.
- `mode=max` keeps every intermediate layer. The lifecycle policy expires
  untagged (superseded) cache manifests after 1 day, and an app's cache after
  `build_cache_expiry_days` (default 30) without a release.
- Measured locally (amd64 emulated on an arm Mac, local registry):

  | Build | Result |
  |---|---|
  | processor-ieeg-report v0.1.1, cold | 24–26 s |
  | processor-ieeg-report v0.1.1, cache only (fresh daemon) | 1 s, same digest |
  | processor-seizure-dynasd v0.2.0, cold | 325 s (kaniko on Fargate: 535 s) |
  | dynasd v0.2.2 with the v0.2.0 cache | **33 s** (kaniko: 318 s) |

  The v0.2.0 heredoc step wrote `/opt/dynasd/ONCET/*` into its layer.

### Compute

`BUILD_GENERAL1_LARGE`: 8 vCPU, 15 GB of memory and 128 GB of disk, Linux
container, privileged.

- **Disk:** the largest known build (dynasd, a 3.8 GB CUDA image) used 10.8 GB
  of BuildKit state. Unlike kaniko, BuildKit keeps no unpacked copy of the
  filesystem and takes no snapshots. 128 GB covers the 3.8–16 GB CUDA images,
  and also the 100 GiB that kaniko's GPU builds get today.
- **Larger builds:** a build whose `APP_STORAGE` exceeds 128 runs on
  `BUILD_GENERAL1_XLARGE` (36 vCPU, 72 GB, 256 GB) through
  `computeTypeOverride`. With today's GPU value of 100, no build does.
- **Memory:** 15 GB against kaniko's 24 GiB. kaniko needed the memory for its
  snapshots; BuildKit RUN steps such as pip installing torch fit in 15 GB.
- **Image:** `aws/codebuild/amazonlinux-x86_64-standard:5.0`. It is
  AWS-maintained, cached on the build hosts, and ships git, curl and the aws
  CLI. BuildKit comes from the pinned tarball, not from Docker Hub.
- **BuildKit state:** build.sh puts it on `/codebuild/output`, or on
  `/var/lib/docker` when that is not overlay, so the `overlayfs` snapshotter
  works. On an overlay root, buildkitd falls back to the slower `native`
  snapshotter. The build log prints which one is in use; check it on the
  first dev build.

### Security

- **Privileged mode** is needed for buildkitd to run with its process sandbox
  on. The flag only applies inside this build's own CodeBuild environment.
  Each build gets a fresh environment, matching kaniko's "one Fargate task per
  build" boundary.
- **The sandbox:** each RUN step gets its own PID and mount namespaces.
  Checked: a RUN step sees only its own processes, not buildkitd and not the
  build's environment. buildctl holds the registry credentials client-side.
  Under kaniko, a RUN step ran inside the builder itself and could read
  `/proc/1/environ` (AWS credentials, `GIT_TOKEN`).
- **Service role** (`aws_iam_role.image_builder`), and nothing else:
  - `ecr:GetAuthorizationToken`;
  - push to appstore-private, plus `BatchGetImage`, which the push needs to
    check whether a manifest exists;
  - read and write on the cache repository;
  - `CreateLogStream` and `PutLogEvents` on its own log group;
  - `ssm:GetParameters` on the token path;
  - with `image_builder_dockerhub_auth` (default true), read on the Docker
    Hub credentials secret;
  - with `image_builder_dockerhub_cache` (default true), pull and import on
    `repository/docker-hub/*`, the ECR pull-through cache for Docker Hub.
    build.sh makes it buildkitd's `docker.io` mirror (`DOCKERHUB_MIRROR`), so
    base images are pulled from Docker Hub once, with the platform's account,
    and read from ECR afterwards. On any mirror error buildkitd falls back to
    Docker Hub (checked locally: a 401 from the mirror moves on to
    registry-1.docker.io). The rule reads a copy of the Docker Hub secret
    named `ecr-pullthroughcache/...`, as ECR requires.

  The provisioner gains `codebuild:StartBuild` on the project, and
  `ssm:PutParameter` and `ssm:DeleteParameter` on the token path. The status
  lambda gains `codebuild:BatchGetBuilds`, `logs:GetLogEvents` on the build
  log group, and `ssm:DeleteParameter` on the token path.
- **Behaviour change:** kaniko ran with the provisioner's credentials, which
  can pull from any ECR repository in the account. A Dockerfile whose `FROM`
  names a private ECR image in the Pennsieve account builds under kaniko but
  not under CodeBuild. Public base images are unaffected.

### The kaniko guard

While `image_builder = "kaniko"`, the service lambda reads the release's
`Dockerfile` before it starts the build (`handler/dockerfile_check.go`). It
refuses the release (HTTP 422, version status `error: ...`) when a
`RUN`/`COPY`/`ADD` uses a heredoc (`<<EOF`, `<<-'EOF'`, ...) or a `RUN` uses
`--mount`. The message names the lines. kaniko would silently build something
other than what the author tested: it dropped the dynasd v0.2.0 heredoc body,
so `/opt/dynasd` was never created. With `codebuild`, the check is skipped. A
Dockerfile that can't be read doesn't block the release.

## Environments

The module is environment-agnostic:
- names come from `environment_name`, `service_name` and the region;
- the appstore-private repository comes from platform-infrastructure's
  outputs;
- the log group takes an optional KMS key and retention.

Every CodeBuild resource has `count = image_builder == "codebuild"`, so with
kaniko the plan only changes the pinned kaniko image and the new environment
variables.

**clin** does not use `terraform/` from this repo. clin-infrastructure has its
own copy of the module (`modules/app-deploy-service`), which:
- runs images from clin ECR, pinned by digest (`third-party/kaniko` v1.24.0
  already);
- encrypts log groups with the accelerator KMS key and keeps them 365 days;
- uses `app_subnet_ids`;
- names resources from `aws_account`;
- uses AWS provider v6 (`aws_region.region`).

Switching clin means porting `codebuild.tf`, `image-builder/`, the IAM
statements and the environment variables into that module, with
`image_builder_log_kms_key_arn` set to the accelerator key. Whether clin may
download BuildKit from GitHub or needs a mirror (S3 or clin ECR) is for clin
to decide. The build already clones the app from GitHub.

## Rollout

1. **Merge.** Nothing changes: `image_builder` defaults to `kaniko`, which is
   now pinned to v1.24.0 by digest, and the heredoc/`--mount` guard is on. The
   status lambda handles both event types.
2. **dev:** set `image_builder = "codebuild"` in dev's service-deploy
   variables and apply. This creates the cache repository, the log group, the
   role, the project and the event rule.
3. **Test on dev:**
   - release a test app (ieeg-report), then a CUDA app (dynasd);
   - check the build time and the snapshotter line in the build log;
   - check that `<tag>-soci` appears in appstore-private and that
     `image_resolver` returns the SOCI digest;
   - run a workflow with the app on Fargate;
   - release the same app again to see the cache hit;
   - release from a private repo;
   - break a Dockerfile and check the deployment's `stoppedReason`.
4. **prod**, then **clin** (after porting the module).

**Rollback:** set `image_builder = "kaniko"` and apply. The provisioner and
the service lambda pick the variable up from their environment, the guard
comes back, and the CodeBuild resources are destroyed. The cache repository
has `force_delete`, and its contents are only cache. Images already pushed are
untouched.

## Cost

CodeBuild on-demand Linux in us-east-1 (AWS price list, 2026-10):
- `general1.large`: $0.02 per minute;
- `general1.xlarge`: $0.0798 per minute;
- billed per minute.

Today's kaniko task (4 vCPU / 24 GiB Fargate) costs about $0.0045 per minute.

- dev ran 72 builds in 45 days (about 50 a month), with a median of 47 s of
  kaniko time.
- With CodeBuild, assume about 1 minute of provisioning and BuildKit install
  per build, plus the build. Most releases hit the cache.
- Estimate: 50 builds × ~3 min × $0.02 ≈ **$3/month**. Worst case, every
  build a cold 20-minute CUDA build: ≈ $20/month.
- Cache storage: ECR at $0.10 per GB-month, a few GB per CUDA app, so a few
  dollars a month.
- Prod and clin build less often than dev.

## Testing locally

build.sh runs as-is in the rootful BuildKit image against a local registry:

```sh
docker network create cbtest
docker run -d --name cbtest-registry --network cbtest -p 5066:5000 registry:2.8.3
printf '[registry."cbtest-registry:5000"]\n  http = true\n' > buildkitd.toml
docker run --rm --privileged --network cbtest \
  -v $PWD/buildkitd.toml:/etc/buildkit/buildkitd.toml:ro \
  -v $PWD/terraform/image-builder/build.sh:/build.sh:ro \
  -e CONTEXT='https://github.com/pennsieve-apps/processor-seizure-dynasd#refs/tags/v0.2.0' \
  -e DESTINATION=cbtest-registry:5000/appstore-private:1-v0.2.0 \
  -e BUILD_CACHE_REF=cbtest-registry:5000/appstore-buildcache:app-dynasd \
  -e BUILDKITD_FLAGS='--config /etc/buildkit/buildkitd.toml' \
  --entrypoint sh moby/buildkit:v0.34.1 /build.sh
```

The first build logs `failed to configure registry cache importer: ... not
found`. That is expected: there is no cache yet.
