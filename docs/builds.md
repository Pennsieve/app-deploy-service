# App Store image builds

When a release is published to the App Store, this service builds the
release's root `Dockerfile` from git at the release tag and pushes the image to
`<env>-appstore-private-<region>` as `<hash>-<tag>`. The SOCI index builder
then adds `<hash>-<tag>-soci`, and `image_resolver` hands out the SOCI-enabled
digest.

BuildKit v0.34.1 in a CodeBuild project (`terraform/codebuild.tf`) does the
build: the same engine as `docker build`, with a per-app registry cache.
`image_builder` is `codebuild`, its only value.

**kaniko is retired.** It was the first builder (a Fargate task, archived
upstream, which ignored Dockerfile heredocs and `RUN --mount`); every
environment moved to CodeBuild, and the kaniko task, its guard against
BuildKit-only Dockerfiles and the `image_builder = "kaniko"` option are gone.
The comparisons with kaniko below are kept as the record of why.

Background: the spike in [buildkit-spike.md](buildkit-spike.md). It explains why
BuildKit cannot run on Fargate and why CodeBuild was chosen.

## Flow

```
release ─► service lambda (post_appstore_handler) ─► provisioner task (ADD_TO_APPSTORE)
                │                                                    └─ codebuild.StartBuild
                ▼
        version "registering" ─► "deploying" ─► status lambda ─► "deployed" | "error"
                                                   ▲
              CodeBuild Build State Change ────────┘
```

With `image_builder_staging` on, the build pushes to a staging repository
and the status lambda promotes the image to appstore-private before the
version is told it is deployed (see "Isolation").

## Isolation

Builds run app authors' code (their Dockerfile's `RUN` steps), so the
builder is confined on three sides (Platform Maintenance ticket 868mvbq5b;
probe: Pennsieve/test-egress-probe, branch `build-probe`):

- **Credentials.** `RUN` steps run in BuildKit's process sandbox (own PID
  and mount namespaces) and on BuildKit's bridge network (own network
  namespace); build.sh drops forwarded traffic to the container-credentials
  endpoint 169.254.170.2 and the instance metadata endpoint
  169.254.169.254 before buildkitd starts. See "RUN steps are isolated"
  under the CodeBuild path.
- **What the build's role can write.** With `image_builder_staging` on, a
  build pushes to `<env>-appstore-staging-<region>:<deploymentId>`
  (`DESTINATION`) and names its App Store image in `PROMOTE_TO`; the
  builder role can write only staging and the build cache. The status
  lambda, platform code, copies a SUCCEEDED build's image to its
  destination (`lambda/status/handler/promote.go`, go-containerregistry,
  digest preserved), then deletes the staging tag; the staging
  repository's lifecycle expires anything left after a day. A promotion
  that fails fails the build (`PROMOTE_FAILED`, the reason on the
  deployment), so a deployed version always names a promoted image. Turn
  the variable on after a status lambda with promotion is deployed; off,
  builds push straight to appstore-private as before.
- **Egress.** With `image_builder_in_vpc` on, builds run in a VPC of their
  own (`build-vpc.tf`): private subnets behind one NAT gateway, an S3
  gateway endpoint, a security group that allows only HTTP, HTTPS and DNS
  out, and a Route 53 Resolver DNS Firewall that resolves only
  `image_builder_allowed_domains` (Pennsieve's own public domains, package
  indexes, code hosts, registries, AWS, the review block's dataset hosts,
  model hubs) and answers NXDOMAIN
  to everything else, failing closed. Every query is logged
  (`/aws/route53resolver/<build vpc>`) and so is every flow
  (`/aws/vpc/<build vpc>-flow`): what a build tried to reach is on record.
  A build cannot reach the platform VPC at all. The one gap is a
  connection by raw IP on 80 or 443, which the flow log shows; an egress
  proxy would close it if it is ever seen. The build timeout is 30 minutes.
  To extend the allow-list for an app, add the domain to the environment's
  `image_builder_allowed_domains`. The list is matched against the name a
  build asks for; the CNAME chain behind an allowed name is trusted
  (`TRUST_REDIRECTION_DOMAIN`), so a listed host served through a CDN
  (auth.docker.io is a CNAME into cloudflare.net) resolves without the
  CDN being listed.

The build's inputs: the source URL, the tag, the destination
`<appstore-private>:<hash>-<tag>`, a GitHub token for a private repo, and the
storage a GPU build needs (`APP_STORAGE` from app.yml). Its
output is the image pushed to that tag. The deployment record and the version
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
- **Build** (`terraform/image-builder/build.sh`, inlined into the buildspec
  with `buildkitd.sh`, the registry login and buildkitd start it shares
  with fixtures.sh):
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
- **RUN steps are isolated.** The process sandbox gives each `RUN` step its
  own PID and mount namespaces; `--oci-worker-net=bridge` gives it its own
  network namespace on BuildKit's bridge (`buildkit0`, 10.10.0.0/16, the CNI
  plugins from the release tarball), and build.sh drops traffic from the
  bridge to the container-credentials endpoint 169.254.170.2 and the
  instance metadata endpoint 169.254.169.254 before buildkitd starts, in
  raw `PREROUTING` (before the agent's nat DNAT of 169.254.170.2:80 to its
  local port, which a `FORWARD` drop never sees) and in `FORWARD`. Probes on
  2026-11-11 (Pennsieve/test-egress-probe, branch `build-probe`) showed first
  that BuildKit's default "auto" mode shared the build container's network
  and could reach 169.254.170.2, then that the bridge with only the
  `FORWARD` drop still got HTTP 404/400 from it. The step keeps internet
  access for package indexes and code hosts. build.sh refuses to build when
  the worker does not report `network: cni`; `RUN_NETWORK=host` is an
  explicit override for a diagnosis. Re-run the probe (a pre-release of that
  branch, with a bumped version line so the step is not served from cache)
  after any change here.
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

### The Lambda step runtime

Every image the CodeBuild path builds carries the **Lambda step runtime**
(compute-node-aws-provisioner-v2, `cmd/lambda-step-runtime`): a static binary
at `/opt/pennsieve/lambda-step-runtime` and the label
`net.pennsieve.lambda-step-runtime=<version>`. A compute node runs an image on
Lambda by making the runtime the function's entrypoint; the runtime fetches
the step's inputs into the function's `/tmp`, runs the image's own entrypoint
and command exactly as an ECS step would be run, and pushes the outputs. An
app needs no Lambda-specific code. A node refuses to run an image without the
label on Lambda ("rebuild the app").

- `lambda_step_runtime_version` (Terraform) names the provisioner release
  whose layer image, `<registry>/pennsieve/lambda-step-runtime:<version>` in
  pennsieve-cc ECR, is used. Empty (the default), images are built without
  it. Bump it when the provisioner releases a new runtime; images already
  built keep the version they were built with, which the node accepts.
- build.sh reads the release's Dockerfile from a shallow clone of the context
  (the private-repo token goes to git through `GIT_ASKPASS`) and adds two
  lines at the end of the stage the build makes (`BUILD_TARGET`, else the
  last stage):

  ```
  COPY --from=<registry>/pennsieve/lambda-step-runtime:<version> /opt/pennsieve/lambda-step-runtime /opt/pennsieve/lambda-step-runtime
  LABEL net.pennsieve.lambda-step-runtime=<version>
  ```

  buildctl then takes the Dockerfile from that local directory
  (`--local dockerfile=` with `--opt dockerfilekey=dockerfile`, which makes
  the frontend read it there rather than from the remote context) while the
  build context stays the git URL. Done in
  the Dockerfile rather than after the push so the tag is pushed once, as the
  final image: appstore-private's tags are immutable, and the SOCI index
  builder indexes what was pushed.
- **Refused** at build, with the reason in the version's status: a Dockerfile
  whose built stage is based on an AWS Lambda base image
  (`public.ecr.aws/lambda/...`, `amazon/aws-lambda-...`) without an
  `ENTRYPOINT` of its own, or whose `ENTRYPOINT`/`CMD` runs `awslambdaric` or
  `lambda-entrypoint.sh`. Under the runtime those start the Runtime Interface
  Emulator and wait forever. An app that used such an image keeps working by
  switching to a plain base image and a normal entrypoint.
- The CodeBuild role gets pull on the runtime's repository
  (`LambdaStepRuntimePull`); the repository's own policy admits any principal,
  as the compute-node sidecars' do.
- **kaniko builds get no runtime.** Environments still on `kaniko` must move
  to `codebuild` before their compute nodes are updated to a provisioner
  release that runs Lambda steps through the runtime.
- **Backfill:** versions built before the runtime are given the layer in
  place by `scripts/backfill-lambda-step-runtime.sh`, which appends the layer
  and the label with `crane`, pushes the result under a temporary `<tag>-rt`,
  deletes the old tag and its `-soci` index (tags are immutable, and ECR
  deletes an image with its last tag, so the new image is in the repository
  before the old one goes), tags the new image `<tag>` and drops the
  temporary tag; the SOCI index builder re-indexes the push. The list to
  backfill is the versions whose `computeTypes` include `lambda`. Run it per
  environment before its nodes are updated.

### Lambda eligibility

Because every image carries the runtime, authors don't opt in to Lambda:
app.yml's `runtime.computeTypes` declares only `standard` and/or `gpu`
(`lambda` is still accepted, and no longer needed), and the platform decides
from facts whether a version (each variant) also runs as a Lambda step. The
author can opt out with `runtime.optimize: false` (top level or a variant's
runtime; default true): the app then runs only as `computeTypes` declares it,
never on a runtime Pennsieve picks (Lambda today, others later).

When a build succeeds the status lambda reads the image it pushed, by digest
(ECR `BatchGetImage`, the linux/amd64 manifest of an image index, then the
config blob through `GetDownloadUrlForLayer`), and records `lambda` next to
`imageDigest`, in the same update, on the version and/or `variants.<id>`:

```json
{"eligible": false, "reasons": ["it runs on GPU only"], "imageBytes": 2147483648, "checkedAt": "2026-11-03T12:00:00Z"}
```

`imageBytes` is the compressed size (the sum of the layers). `overridable`
is true (and otherwise omitted) when the version isn't eligible only because
of its declared `memory`, `cpu`, `timeoutSeconds` or `diskGiB`, so a user may choose
Lambda for a run anyway; any other reason makes it false. A version is
**not** eligible, with a reason for each that applies, when:

| Rule | Reason |
|---|---|
| `runtime.optimize: false` | the app opts out of optimized runtimes (runtime.optimize: false) |
| `computeTypes` is `[gpu]` | it runs on GPU only |
| interactive | interactive sessions don't run on Lambda |
| `memory` > 10240 MiB | it needs N GB of memory; a Lambda step has at most 10 GB |
| `cpu` > 6144 units | it needs N vCPU; a Lambda step has at most 6 |
| `timeoutSeconds` > 900 (unset is fine; 900, Lambda's own limit, fits, and the compute node still stops a step at 840 s) | it may run up to …; a Lambda step runs at most 15 minutes |
| `diskGiB` > 10 (a Lambda step's /tmp is 10240 MB; 10 or less fits) | it needs N GiB of local disk; a Lambda step has at most 10 GB |
| image > 10 GB compressed | the image is N GB; Lambda takes images up to 10 GB |
| no `net.pennsieve.lambda-step-runtime` label | the image has no Lambda step runtime: rebuild the app |
| not linux/amd64 | the image is not linux/amd64 |

The image limits are the ones compute-node-aws-provisioner checks when it
creates a step's function (`internal/aslconverter/lambda_image.go`); the rules
are `lambda/status/lambdaeligibility`. A failure to read the image is logged
and leaves `lambda` unset; the digest and the build's status are recorded as
usual. Versions built before this are decided by
`lambda/status/cmd/backfill-lambda-eligibility` (`APPSTORE_VERSIONS_TABLE=...
go run ./cmd/backfill-lambda-eligibility [-dry-run] [-versions uuid,...]`;
`-versions` limits it to those versions and their variants), which reads each
version's and variant's image by its recorded digest (else its tag); it can
be rerun, and should be after `runtime.optimize` changes for a stored manifest
(for example after `backfill-manifests`).

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
  - with `image_builder_dockerhub_cache` (default false), pull and import on
    `repository/docker-hub/*`, the ECR pull-through cache for Docker Hub.
    build.sh makes it buildkitd's `docker.io` mirror (`DOCKERHUB_MIRROR`), so
    base images are pulled from Docker Hub once, with the platform's account,
    and read from ECR afterwards. On any mirror error buildkitd falls back to
    Docker Hub (checked locally: a 401 from the mirror moves on to
    registry-1.docker.io). The rule itself is not created by this module:
    it needs an upstream credential (`credential_arn`), which AWS provider 4.x
    lacks, and the environments run this module on 4.67. Create it where the
    provider is 5.x (platform-infrastructure), with a secret named
    `ecr-pullthroughcache/...` holding `username` and `accessToken`, then turn
    `image_builder_dockerhub_cache` on.

  The provisioner gains `codebuild:StartBuild` on the project, and
  `ssm:PutParameter` and `ssm:DeleteParameter` on the token path. The status
  lambda gains `codebuild:BatchGetBuilds`, `logs:GetLogEvents` on the build
  log group, and `ssm:DeleteParameter` on the token path.
- **Behaviour change:** kaniko ran with the provisioner's credentials, which
  can pull from any ECR repository in the account. A Dockerfile whose `FROM`
  names a private ECR image in the Pennsieve account builds under kaniko but
  not under CodeBuild. Public base images are unaffected.

## Variants

An app.yml with `variants` ([app-manifest-variants.md](app-manifest-variants.md)) builds
one image per variant on every release. An app.yml with neither `variants` nor `build`
builds exactly as before: the same single build, tag, variables and statuses.

| | Image tag | SOCI index | Build cache |
|---|---|---|---|
| App without variants | `<hash>-<tag>` | `<hash>-<tag>-soci` | `app-<key>` |
| Default variant | `<hash>-<tag>` | `<hash>-<tag>-soci` | `app-<key>` |
| Other variant `gpu` | `<hash>-<tag>--gpu` | `<hash>-<tag>--gpu-soci` | `app-<key>--gpu`, also imports `app-<key>` |

- **Registration** (service lambda): stores the variants with one deployment per built
  variant (the default variant's is the release's), and still starts **one** provisioner
  task, with `READ_VERSION_BUILDS=true`. `APP_VARIANTS_ENABLED=false` (Terraform
  `app_variants_enabled`) builds only the default variant; the others then stay without
  a status, and the registry calls them not yet deployed.
- **Provisioner**: reads the variants and build settings from the version and calls the
  builder once per variant, the default first. Each build gets its variant's merged
  `build` and its own disk (`buildStorageGiB`). A top-level `build` without variants
  applies to the single image.
  - **kaniko**: `--build-arg K=V` per arg (sorted), `--target`, `--dockerfile`; the task
    is tagged `Variant` and `VariantDefault`.
  - **CodeBuild**: the variables `VARIANT`, `VARIANT_DEFAULT`, `BUILD_ARGS` (one `K=V`
    per line, sorted), `BUILD_TARGET`, `DOCKERFILE` and `BUILD_CACHE_IMPORT_REF`.
    build.sh passes them to buildctl as `--opt build-arg:K=V`, `--opt target=…`,
    `--opt filename=…` (read from the git context) and a second `--import-cache`.
  - Settings are checked before a build starts: arg names as the schema allows, values
    on one line, a relative Dockerfile path without `..`.
- **Status** (status lambda): reads the variant from the task tags or build variables
  and sets `variants.<id>.status` (`deployed`/`error`) and `failureReason` (the build's
  stopped reason; for kaniko with the exit code). The **default** variant also sets the
  version's `registrationStatus`, as today. A build without a variant is recorded as
  before.
- **Partial failure**: a failed **non-default** variant (its build fails, or doesn't
  start) leaves the version `deployed`; only that variant is `error` and refused by the
  registry. A failed default variant fails the version as before; if it doesn't even
  start, the other variants aren't built.
- **Live updates**: the version's status (`application_status_event`) names the default
  variant in `variant`. Other variants' statuses are `application_variant_status_event`
  on the same channel, so a failed GPU build never reads as the version's status.

## What a version records about its build

So a review verdict can be tied to the exact code and image tested, a version records:

| Field | Set by | What it is |
|---|---|---|
| `commitSha` | service lambda, at registration | The commit the release tag points to (GitHub `GET /repos/{owner}/{repo}/commits/{tag}`), so a tag moved later doesn't change what was built. Empty when GitHub couldn't say; a failed lookup is logged and never blocks the release. |
| `imageDigest` | status lambda, when the build succeeds | The digest ECR gives the pushed tag (`DescribeImages`): the image of an app without variants, or of the default variant. |
| `variants.<id>.imageDigest` | status lambda, when that variant's build succeeds | The variant's image digest (`<hash>-<tag>--<id>`; the default variant's is also the version's `imageDigest`). |
| `lambda`, `variants.<id>.lambda` | status lambda, with the digest | Whether the version (variant) can run as a Lambda step, and why not ([Lambda eligibility](#lambda-eligibility)). |
| `review` | service lambda, at registration | app.yml's `review` block ([app-manifest-review.md](app-manifest-review.md)) as parsed: what a review run uses. Parts that don't parse are left out (the schema reports them). |
| `fixtures` | status lambda, after the version is deployed | The release's fixture bundle ([Fixture bundles](#fixture-bundles)): `status` building, ready, skipped or failed, the bundle's `bucket`, `key`, `bytes` and `sha256`, `warnings`, and an `error` code and message when there is no bundle. |

The digest is the tag's at the moment the build ends, recorded before the status says
`deployed`. A digest that can't be read (no `destinationUrl`, an ECR error, the tag not
found) is logged as a warning and the build's status is recorded as usual. All of them are
in the API's version model. Versions registered before these were kept have none of them.

## Fixture bundles

A sample run (App Sample Documents epic) needs the app's declared test
inputs, the `review` block of app.yml ([app-manifest-review.md](app-manifest-review.md)).
Building them inside the run needs internet on the compute node and repeats
every fetch; building them once at release, where the code already runs with
internet under the sandbox above, makes sample runs credential-free and
network-free and bounds the cost to once per release.

- **What.** With `fixture_builder` on (needs `image_builder_in_vpc` and
  `app_fixtures_image`), a second CodeBuild project,
  `<env>-app-deploy-service-fixture-builder`, in the build VPC with the same
  security group and BuildKit install, runs `image-builder/fixtures.sh`. It
  builds a two-stage Dockerfile: `FROM` the platform's own
  [app-fixtures](https://github.com/pennsieve-apps/app-fixtures) image
  (pinned per environment), a `RUN` step that executes app-fixtures with
  `REPOSITORY`, `TAG`, `PORTS` and the caps as build args, and a `scratch`
  stage holding only its output, exported locally (`--output type=local`,
  `--no-cache`, no push). The generators and dataset fetches app-fixtures
  runs are the app author's code, which is why they run as a `RUN` step:
  own network namespace on the bridge, the credential and metadata
  endpoints dropped, the DNS Firewall allow-list (which lists the dataset
  hosts), 80 and 443 only. fixtures.sh, platform code, only tars what the
  step exported and uploads it; nothing from the export is executed or put
  on a command line.
- **Where.** Bucket `<env>-app-deploy-service-fixtures-<region>` (private,
  SSE-S3, public access blocked; platform-internal, never published, never
  on Discover). Under `fixtures/<applicationId>/<tag>/`: the bundle
  `<sha256>.tar.gz` (the target app's `INPUT_DIR` layout plus
  `.pennsieve-fixtures/` with the provenance crate or `error.json`) and
  `manifest.json`, always written: `status` ready or failed, the bundle's
  key, bytes and sha256, `warnings`, the `error` code and message
  (app-fixtures' codes such as `no_review_block`, `dataset_too_large`,
  `generator_failed`, or `too_large` from the cap), the ports built, the
  app-fixtures image, the build id, and the provenance crate inline. The
  log carries one `FIXTURES_RESULT` line with the manifest. The build is
  SUCCEEDED whenever a manifest was written, failed only when fixtures.sh
  itself failed (buildkit, upload).
- **Caps** (variables with these defaults): a compressed bundle over
  `fixture_bundle_max_bytes` (2 GiB) is not uploaded and the manifest says
  `too_large`; over `fixture_bundle_warn_bytes` (200 MiB) it is uploaded
  with a warning; one declared dataset over `fixture_dataset_max_bytes`
  (2 GiB) fails its port in app-fixtures; a generator may run
  `fixture_generate_timeout_seconds` (1200); the build
  `fixture_builder_timeout_minutes` (30) on
  `fixture_builder_compute_type` (`BUILD_GENERAL1_MEDIUM`, 7 GB).
- **Retention.** The status lambda tags an app's bundles older than its
  previous release `retain=no`; the bucket's lifecycle expires those after
  `fixture_bundle_expiry_days` (30). The current and previous release's
  bundles stay. Bundles are content-addressed, so an unchanged review block
  costs no storage twice.
- **Role** (`aws_iam_role.fixture_builder`), and nothing else: ECR login;
  pull from appstore-private (app-fixtures lives there); `s3:PutObject`
  under `fixtures/` of its bucket; its own log group; the ENI permissions
  every VPC build needs. No staging push, no SSM, no secrets, no git token
  (private repositories are out of scope: app-fixtures clones the app).
- **Status lambda** (`lambda/status/handler/fixtures.go`). When a release's
  image is deployed (built, promoted, its digest recorded; an app without
  variants or its default variant), the lambda reads the version and its
  App Store application and starts the fixture build with `REPOSITORY`,
  `TAG`, `APPLICATION_ID`, `VERSION_ID` and `APPLICATIONS_TABLE`, recording
  `fixtures.status = building` on the version. It starts none, records why
  and sends the release event at once when the version has no `review`
  block (`no_review_block`), the repository is private
  (`private_repository`: app-fixtures clones it and has no token) or the
  app is app-fixtures itself (`platform_app`). The fixture build's own
  state change (the same EventBridge rule, matched by project name) ends
  here too: the lambda reads `manifest.json`, records
  `fixtures {status, bucket, key, bytes, sha256, files, warnings, error,
  startedAt, builtAt, buildId}` on the version (`failed` with
  `build_<status>` and the log tail when the build itself failed, or
  `manifest_missing`), tags the app's bundles older than its previous
  release `retain=no`, and sends the release event. Every change of the
  record is also published on the version's realtime channel as
  `application_fixtures_status_event` (status, error code, bytes, files),
  next to the build's `application_status_event`, so the app page follows
  the whole release live. Nothing here changes a version's deployed status
  or fails the image build's event.
- **Release event.** Once per version, when the image is deployed and the
  fixtures are decided (ready, skipped or failed): EventBridge default bus,
  source `pennsieve.app-deploy-service`, detail-type `App Release Ready`,
  detail `{environment, applicationId, versionId, tag, imageDigest,
  sourceUrl, visibility, isPrivate, fixtures{...}}`, resources
  `[versionId]`. curation-service subscribes to it for sample documents;
  `applicationId` and `tag` are its idempotency key.
- **Locally:** fixtures.sh runs in the rootful BuildKit image like build.sh
  (see [Testing locally](#testing-locally)), with `APP_FIXTURES_IMAGE`
  pointing at a local registry's copy of app-fixtures and
  `FIXTURES_BUCKET` empty, which leaves the bundle and manifest in
  `/tmp/fixtures-build/`; the image needs `python3` (`apk add python3`).

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

## Rollout (done; kaniko since retired)

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

There is no rollback to kaniko any more: the kaniko task and its option were
removed once every environment built with CodeBuild.

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
  -v $PWD/terraform/image-builder/buildkitd.sh:/buildkitd.sh:ro \
  -v $PWD/terraform/image-builder/build.sh:/build.sh:ro \
  -e CONTEXT='https://github.com/pennsieve-apps/processor-seizure-dynasd#refs/tags/v0.2.0' \
  -e DESTINATION=cbtest-registry:5000/appstore-private:1-v0.2.0 \
  -e BUILD_CACHE_REF=cbtest-registry:5000/appstore-buildcache:app-dynasd \
  -e BUILDKITD_FLAGS='--config /etc/buildkit/buildkitd.toml' \
  --entrypoint sh moby/buildkit:v0.34.1 /build.sh
```

The first build logs `failed to configure registry cache importer: ... not
found`. That is expected: there is no cache yet.
