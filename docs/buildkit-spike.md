# Spike: BuildKit instead of kaniko for App Store image builds

Status: spike, 2026-10-29. Nothing deployed; no shared infrastructure changed.

> Kept as background for [builds.md](builds.md), which describes what was built
> (CodeBuild + BuildKit, option B). The prototype `fargate/buildkit-builder/`
> mentioned below stayed in the spike PR (#203) and was not merged; its
> `build.sh` became `terraform/image-builder/build.sh`.

## TL;DR

- **BuildKit does not run on Fargate.** Every BuildKit mode needs either a user
  namespace (rootless) or `CAP_SYS_ADMIN` for mounts (rootful). The default
  seccomp profile blocks the first. Fargate does not grant the second: it allows
  no privileged mode, no `dockerSecurityOptions` (so no custom seccomp), no
  devices (`/dev/fuse`), and only `SYS_PTRACE` as an added capability.
  Reproduced locally under the same constraints. The one recipe that works,
  rootless with `seccomp=unconfined`, is not available on Fargate.
- **Where seccomp can be relaxed, BuildKit fixes the bug and is much faster
  on releases.** dynasd v0.2.0 now produces `/opt/dynasd` from its heredoc. With
  a registry cache, a new dynasd release rebuilt in **35 s**; kaniko took **318 s**
  for the same release on Fargate, and kaniko never caches today.
- **Recommendation:** move image builds to **CodeBuild running BuildKit**
  (option B). Keep the provisioner's inputs and outputs. Until then, pin kaniko
  by digest and reject Dockerfile heredocs at release time, so the platform fails
  loudly instead of building a different image.

## What runs today

| | |
|---|---|
| Builder image | `gcr.io/kaniko-project/executor:latest`: Google's **archived** kaniko (archived 2025-06-03). `latest` currently resolves to **v1.24.0** (`sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698`). It is not Chainguard's fork. The tag is unpinned (`terraform/variables.tf` `deployer_image_url`/`deployer_image_tag`). |
| Task | Fargate, 4 vCPU / 24 GiB (`dev-app-deploy-service-app-deployer-task-use1` rev 10). Ephemeral storage defaults to 20 GiB; `APP_STORAGE` overrides it (100 GiB for GPU apps, `lambda/service/handler/app_config.go`). |
| Invocation | `fargate/app-provisioner/main.go` `PrivateDeploy`: `RunTask` with a command override `--context git://…#refs/tags/<tag> --destination <ecr>:<hash>-<tag> --force`. Env: the provisioner's session creds (`AWS_ACCESS_KEY_ID`/`SECRET`/`SESSION_TOKEN`) and `GIT_TOKEN` for private repos. Always the root `Dockerfile`; no `--cache`. |
| Success/failure | `lambda/status` on the ECS *Task State Change* event: container `exitCode != 0` means failed. No log parsing. The image digest is not captured. |
| After the push | The **SOCI Index Builder** (`terraform-modules/platform-infrastructure/soci-index-builder.tf`, awslabs solution) reacts to the ECR push event and pushes `<tag>-soci` (SOCI v2). `image_resolver.go` resolves a tag to the SOCI-enabled manifest digest named in that index. |

### The bug that started this

kaniko 1.x ignores Dockerfile heredocs. The dev log of the dynasd v0.2.0 build
(`/aws/fargate/dev-app-deploy-service-app-deployer-use1`, stream
`fargate/app-deployer/19f127b4…`) shows

```
RUN python3 - <<'PY'
Running: [/bin/sh -c python3 - <<'PY']
```

The body is dropped, so `python3 -` reads an empty stdin, exits 0, and
`/opt/dynasd` is never created. Local `docker build` (BuildKit) handles the same
Dockerfile correctly, so the platform image differed from the image the author
tested. kaniko is archived, so this will not be fixed upstream. Chainguard's fork
promises maintenance only, not new Dockerfile syntax (not verified for heredocs).

### Measured kaniko times (dev, last 45 days)

Taken from the CloudWatch group above, read-only. Times run from kaniko start to
`Pushed`; Fargate scheduling and the image pull of kaniko itself are extra.

- 72 builds: median **47 s**, p90 **264 s**, max **1140 s**; 7 builds over 5 min.
- dynasd (CUDA, 3.8 GB): v0.2.0 **535 s** (pip 338 s, **full-filesystem
  snapshot 141 s**, push 47 s); v0.2.2 **318 s**. Each release rebuilds from
  scratch because kaniko runs without a cache.
- Largest app (`…:1431572384-v1.1.x`): 1111–1140 s.

## Q1: rootless BuildKit under Fargate's constraints

### What Fargate allows (ECS task definition reference, Fargate)

- `privileged`: not supported.
- `linuxParameters.capabilities.add`: **only `SYS_PTRACE`**.
- `devices`: not supported on Fargate, so there is no `/dev/fuse` for fuse-overlayfs.
- `dockerSecurityOptions`: not available on Fargate, so the seccomp profile
  cannot be changed. Fargate applies the runtime's default profile. Independent
  evidence: rootless Podman fails on Fargate with `cannot clone: Operation not
  permitted` (aws/containers-roadmap#1957, open since 2023, no AWS response).
- No `systempaths=unconfined` / `procMount: Unmasked` equivalent.

### What BuildKit needs (moby/buildkit `docs/rootless.md`, `examples/kubernetes`)

The containerised rootless recipe is
`--security-opt seccomp=unconfined --security-opt apparmor=unconfined`. seccomp
must be unconfined to allow `unshare` (runc) and `mount` (snapshotters). The
`systempaths` relaxation can be swapped for `--oci-worker-no-process-sandbox`,
but the seccomp relaxation cannot. The Kubernetes rootless examples also set
`seccompProfile: Unconfined` and `appArmorProfile: Unconfined`.

### Local reproduction

Docker Desktop 29.8.2, linuxkit kernel 7.0.14 (arm64 host, amd64 builds through
binfmt). Containers ran with Docker's default seccomp profile, default
capabilities, no `--privileged` and no devices, which is what Fargate gives a
task. `max_user_namespaces` is 31877, so the kernel allows user namespaces and
only seccomp blocks them. That matches Fargate as far as we can observe it.

| # | Variant | Result |
|---|---|---|
| A | `moby/buildkit:v0.34.1-rootless`, `buildctl-daemonless.sh`, `BUILDKITD_FLAGS=--oci-worker-no-process-sandbox` | **fails**: `[rootlesskit:parent] error: failed to start the child: fork/exec /proc/self/exe: operation not permitted` (`unshare(CLONE_NEWUSER)` → EPERM) |
| B | rootless image, `buildkitd --rootless` without RootlessKit | **fails**: `rootless mode requires to be executed as the mapped root in a user namespace` |
| C | `moby/buildkit:v0.34.1` as root, unprivileged, `--oci-worker-no-process-sandbox` | **refuses**: `can't enable NoProcessSandbox without Rootless` |
| C2 | same, sandbox on, `--oci-worker-snapshotter=native` | **fails**: `failed to mount … rbind ro … operation not permitted` (needs `CAP_SYS_ADMIN`) |
| C3 | C2 + `--cap-add SYS_PTRACE` (the only cap Fargate adds) | **fails** the same way |
| D | rootless + `seccomp=unconfined` + `apparmor=unconfined` + `--oci-worker-no-process-sandbox` (the docs' recipe) | **works**, but uses options Fargate does not allow |

**Verdict: not viable on Fargate.** Only a real Fargate task can prove this 100%:
a 2-minute `unshare --user true` probe in a sandbox account. It was not run here
because the spike was not allowed to deploy anything. Variants A–C fail for
reasons Fargate cannot work around, so the probe would at best confirm the result.

### Security comparison (for where BuildKit can run)

| | kaniko on Fargate (today) | rootless BuildKit, no-process-sandbox | BuildKit with process sandbox (CodeBuild privileged, or rootful) |
|---|---|---|---|
| Builder runs as | root in the container | uid 1000, root only inside its own user namespace | root in an isolated build VM/container |
| RUN step filesystem | the builder's own rootfs: kaniko unpacks the image over `/` and runs commands in place, so a RUN step can overwrite `/kaniko` | its own rootfs (runc mount namespace); cannot touch the builder binaries | its own rootfs |
| RUN step processes | same PID namespace and uid as kaniko, so it can read `/proc/1/environ` (AWS creds, `GIT_TOKEN`) | same PID namespace and uid as buildkitd (that is what "no process sandbox" means), so it can signal/ptrace buildkitd and read its environ; BuildKit cannot force-kill runaway steps | separate PID namespace; cannot see buildkitd |
| Host-facing relaxation | none; the Fargate microVM is the boundary | `seccomp=unconfined` (+ apparmor): more kernel syscalls reachable, but only as an unprivileged user | privileged inside a single-tenant VM |
| Isolation boundary between builds | one Fargate task per build | whatever hosts the container (needs single-tenant hosts) | one CodeBuild environment per build |

In short, rootless no-sandbox is **no worse than kaniko** inside the build: in
both, a malicious RUN step can read the builder's credentials. It is somewhat
better for the builder's integrity. It needs seccomp opened up, which is only
acceptable when the host is single-tenant per build. In every option the
Dockerfile author runs code next to push credentials, so those credentials must
be scoped to push one destination repo and nothing else. That is already the
case for kaniko today.

## Q2: prototype measurements

Setup: rootless BuildKit **v0.34.1** (variant D), `registry:2.8.3` in place of
ECR, Docker Desktop VM with 15 CPUs and 8 GB RAM. All builds are **linux/amd64
under emulation on an arm64 Mac**, so RUN steps are slower than on native amd64
and the absolute times are pessimistic. Network: home connection to Docker Hub,
PyPI and GitHub; the registry is local, so pushes are faster than they would be
to ECR. Builds ran straight from git, the way the platform does:

```
buildctl build --frontend dockerfile.v0 \
  --opt context=https://github.com/pennsieve-apps/<repo>.git#<tag> \
  --opt platform=linux/amd64 \
  --output type=image,name=<registry>/<repo>:<tag>,push=true \
  --import-cache type=registry,ref=<registry>/<repo>:buildcache \
  --export-cache type=registry,ref=<registry>/<repo>:buildcache,mode=max
```

"Warm" means `buildctl prune --all` first, so all local state is gone and only
the registry cache is used. That is what a fresh task would see.

| Build | BuildKit cold | BuildKit warm (registry cache only) | kaniko on Fargate (dev) |
|---|---|---|---|
| processor-ieeg-report v0.1.1 | 28 s | 0.9 s (same digest) | no matched run; the median kaniko build is 47 s |
| processor-seizure-dynasd v0.2.0 (CUDA, 3.8 GB) | **332 s** (pip 204 s under emulation, export+push 103 s) | **1.3 s** | **535 s** |
| dynasd v0.2.2 with the v0.2.0 cache (a normal new release) | | **34.8 s** | **318 s** |

- **Heredoc:** step `[6/9] RUN python3 - <<'PY'` printed
  `checkpoint ONCET/best_model.pth 521886 bytes, sha256 ok`. The pushed layer
  contains `opt/dynasd/ONCET/best_model.pth` and `final_training_config.json`,
  checked by listing the blob from the registry.
- **Disk:** the dynasd CUDA build used **10.8 GB** of BuildKit state
  (`buildctl du`) for a 3.8 GB compressed image, so 30 GiB is enough and the
  100 GiB GPU override is generous. kaniko needs more because it keeps the
  unpacked filesystem plus snapshots.
- **No full-filesystem snapshot:** kaniko spent 141 s just snapshotting after the
  pip step. BuildKit diffs overlay layers and has no such cost.
- **Prototype wrapper** (`fargate/buildkit-builder/`): `build.sh` takes kaniko's
  exact arguments (`--context git://…#refs/tags/v0.1.1 --destination … --force`),
  rewrites `git://` to `https://` (GitHub turned off `git://`, and kaniko silently
  used https), builds, pushes and exits with buildctl's status. Run end to end
  against the local registry it **pushed and exited 0** (55 s cold). Under
  default seccomp it fails at rootlesskit, as in variant A.

## Q3: ECR auth

- The builder image ships `docker-credential-ecr-login` v0.12.0 with
  `~/.docker/config.json` = `{"credsStore":"ecr-login"}`. buildctl reads the docker
  config on the client side and hands the credentials to buildkitd over the
  session. The helper uses the default AWS chain: the `AWS_*` env vars the
  provisioner already passes, or the task or CodeBuild role.
- Verified: with env credentials, the helper in the prototype image returned an
  ECR token for `941165240011.dkr.ecr.us-east-1.amazonaws.com`
  (`ecr:GetAuthorizationToken` only; nothing was pushed).
- An alternative needs no helper binary:
  `aws ecr get-login-password | …` written into `~/.docker/config.json` `auths`.
  The token lasts 12 h, which covers any build.
- Private repos: `GIT_TOKEN` → `buildctl --secret id=GIT_AUTH_TOKEN,env=GIT_TOKEN`.
  The dockerfile frontend uses that secret to authenticate git contexts.
  Not exercised; the test repos are public.

## Q4: integration

### Output shape and SOCI

- `buildctl` adds **no attestations unless asked** (unlike `docker buildx`,
  which adds provenance by default). The pushed tag was a **single
  `application/vnd.oci.image.manifest.v1+json`** with an OCI config: not an index,
  and no `unknown/unknown` attestation manifest. Keep it that way: do not pass
  `--opt attest:*`, and if buildx is ever used, set `--provenance=false --sbom=false`.
- The SOCI Index Builder (`registry.go` `ValidateImageDigest`, V2) accepts an
  image manifest whose config is `application/vnd.oci.image.config.v1+json` or
  the Docker config type. The OCI single manifest qualifies, so `<tag>-soci` is
  produced as today and `image_resolver.go` needs no change. ECR and Fargate
  pull OCI manifests natively. To mirror kaniko byte-for-byte anyway, add
  `oci-mediatypes=false` to `--output` for Docker schema-2 media types.
- **Cache refs and SOCI:** a registry cache manifest pushed to a tag in
  `*-appstore-private` fires the SOCI builder too. Its config type
  (`application/vnd.buildkit.cacheconfig.v0`) fails validation, so the builder
  logs a warning and skips it: harmless, but noise, and cache tags would mix
  with app tags. Put the cache in a **separate ECR repo**
  (`<env>-appstore-build-cache`, lifecycle policy expiring after ~30 days).
  BuildKit's **S3 cache backend** (`type=s3`) avoids ECR events entirely.

### What changes, per option

The provisioner keeps its inputs (`sourceUrl`, tag, destination, `GIT_TOKEN`,
`APP_STORAGE`) and its output (the image pushed to `<ecr>:<hash>-<tag>`).
`build.sh` also prints `Pushed <repo>@sha256:…` and writes
`/tmp/build-metadata.json`, so the digest can be recorded later if wanted.

**Option B: CodeBuild + BuildKit (recommended)**

- One CodeBuild project per environment (Terraform in this repo), Linux
  `general1.large` (8 vCPU / 15 GB; `xlarge` for GPU apps), **privileged mode
  on**. Its image is the builder image from `fargate/buildkit-builder/` (or the
  stock CodeBuild image running `buildctl-daemonless.sh`). Because the
  environment is privileged, buildkitd can run **with** the process sandbox, so
  RUN steps cannot see the builder's process or credentials.
- `main.go` `PrivateDeploy`: replace `ecs.RunTask` with
  `codebuild.StartBuild`, passing environment overrides `CONTEXT`,
  `DESTINATION`, `GIT_TOKEN` (or better, a Secrets Manager reference),
  `BUILD_CACHE_REF`, `DEPLOYMENT_ID`, `APPLICATION_ID`, `APPLICATIONS_TABLE`,
  plus `computeTypeOverride` from `APP_STORAGE`/GPU.
- Status lambda: add an EventBridge rule for `aws.codebuild` *CodeBuild Build
  State Change* (`build-status` SUCCEEDED / FAILED / STOPPED / TIMED_OUT). Read
  the deployment ids from `additional-information.environment.environment-variables`
  instead of ECS task tags. The success rule stays "exit status 0" as today.
- IAM: a CodeBuild service role that may push only to `appstore-private` and the
  cache repo (`ecr:GetAuthorizationToken`, `BatchCheckLayerAvailability`,
  `InitiateLayerUpload`, `UploadLayerPart`, `CompleteLayerUpload`,
  `PutImage`, `BatchGetImage`), plus CloudWatch Logs. The provisioner gains
  `codebuild:StartBuild` on that project. The session creds no longer have to
  go in plaintext environment overrides.
- Cost: Fargate today is 4 vCPU/24 GiB ≈ $0.27/h ≈ **$0.0045/min**. CodeBuild
  on-demand Linux is billed per minute. `general1.small` is $0.005/min per the
  pricing page; `large` is about $0.02/min (check the current price sheet). At
  dev's rate (72 builds in 45 days, median under 1 min, rare 5–20 min CUDA
  builds), cost is **a few dollars a month** in any option. Caching shortens the
  long builds by ~10x, which more than covers the higher per-minute price.
- Security: each build runs in a fresh CodeBuild environment; the privileged
  flag stays inside it. That matches today's "one Fargate task per build"
  boundary and adds BuildKit's process sandbox. CodeBuild also supports VPC
  placement and timeouts per build.
- Limits: CodeBuild has per-account concurrency quotas for each compute type
  (check them in Service Quotas), and builds queue rather than fail. Measure
  the provisioning latency in dev against Fargate's.

**Option C: ECS on EC2 for the build task**

- Add an EC2 capacity provider (ASG, scale from zero) to the app-deploy
  cluster. Run the prototype image unchanged with
  `dockerSecurityOptions: ["seccomp=unconfined","apparmor=unconfined"]`, which
  is allowed on the EC2 launch type. `main.go` changes only `LaunchType` →
  `capacityProviderStrategy`, and the status lambda is unchanged (still ECS task
  events). It is the smallest code change.
- Drawbacks: we would run and patch an ASG and AMI. Scale-from-zero adds 2–4
  min, or warm instances cost money. Builds must be one per instance
  (task-sized instances), or untrusted Dockerfiles share a host with unconfined
  seccomp. The ephemeral-storage override becomes EBS sizing.

**Option D: stay on kaniko**

Pin by digest and reject heredocs at release (see below). A maintained fork
(Chainguard's, or osscontainertools/kaniko) might add heredoc support, but
kaniko stays a re-implementation of Dockerfile semantics, so the next
divergence is a matter of time. It also keeps the 141 s full-filesystem
snapshots, and its own cache would need `--cache-repo`.

## Risks

- **Divergence never goes away completely:** CodeBuild + BuildKit is the same
  engine as `docker build`, so this class of bug (heredocs, `COPY --chmod`,
  `RUN --mount`, `# syntax=` frontends) goes away. Platform differences
  (amd64 vs a dev's arm Mac) remain. Default `BUILD_PLATFORM=linux/amd64`, as
  the prototype does.
- **Reproducibility:** without `SOURCE_DATE_EPOCH`, identical inputs give new
  digests on an uncached rebuild (seen: ieeg v0.1.1 rebuilt to a different
  digest). That is harmless for us because each release gets a new tag. Setting
  `--opt build-arg:SOURCE_DATE_EPOCH=<commit time>` with
  `rewrite-timestamp=true` would make digests stable.
- **Cache poisoning:** the cache must be **per app** (key = the existing
  source-hash, so `<cache-repo>:<hash>`), never shared across apps or orgs.
  Otherwise one app's Dockerfile could seed layers another app reuses.
- **Cache size:** `mode=max` stores every intermediate layer, about 4 GB for
  CUDA apps. Use a lifecycle policy on the cache repo.
- **Unverified on AWS:** the measurements here were taken under emulation on a
  laptop. Before switching, run the CodeBuild project in dev on dynasd and one
  small app and compare with the kaniko numbers above.
- **SOCI:** the builder skips indexes when an image is "too small"; that
  behaviour is unchanged. Re-check `<tag>-soci` appears after the first
  CodeBuild push in dev.

## Recommendation

1. **Now (small, low risk):** pin the deployer to
   `gcr.io/kaniko-project/executor@sha256:4e7a52dd…` (v1.24.0) instead of
   `:latest`. Fail a release whose Dockerfile contains a heredoc (`<<`
   in a `RUN`/`COPY` instruction) with a clear message that the platform builder
   does not support it yet. That turns a silent wrong image into a loud error.
2. **Next:** **Option B, CodeBuild + BuildKit** (privileged, process sandbox
   on), with a per-app registry cache in a separate ECR repo. Keep the
   provisioner's inputs and the pushed-tag output. Swap `RunTask` for
   `StartBuild` and add a CodeBuild event path to the status lambda. Validate in
   dev, then roll out. Keep the Fargate kaniko path for one release behind a
   flag for rollback.
3. Choose **option C** only if staying on ECS task events outweighs owning an
   ASG. Rootless BuildKit **on Fargate** is not an option today and should not
   be pursued unless AWS adds seccomp/userns controls to Fargate
   (containers-roadmap#1957).

## Reproducing this spike

Images used: `moby/buildkit:v0.34.1-rootless`, `moby/buildkit:v0.34.1`,
`registry:2.8.3`, `gcr.io/kaniko-project/executor:latest` (amd64, for the
version check), and `bkspike-builder:proto` (built from `fargate/buildkit-builder/`).

```sh
docker network create bkspike
docker run -d --name bkspike-registry --network bkspike -p 5055:5000 registry:2.8.3
# buildkitd.toml: [registry."bkspike-registry:5000"] http = true
docker run -d --name bkspike-buildkitd --network bkspike \
  --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
  -v $PWD/buildkitd.toml:/home/user/.config/buildkit/buildkitd.toml:ro \
  moby/buildkit:v0.34.1-rootless --oci-worker-no-process-sandbox
docker exec bkspike-buildkitd buildctl build --frontend dockerfile.v0 \
  --opt context=https://github.com/pennsieve-apps/processor-seizure-dynasd.git#v0.2.0 \
  --opt platform=linux/amd64 \
  --output type=image,name=bkspike-registry:5000/dynasd:v0.2.0,push=true \
  --export-cache type=registry,ref=bkspike-registry:5000/dynasd:buildcache,mode=max

# Fargate-like negative test (default seccomp): fails at rootlesskit
docker run --rm -e BUILDKITD_FLAGS=--oci-worker-no-process-sandbox \
  --entrypoint buildctl-daemonless.sh moby/buildkit:v0.34.1-rootless debug workers
```

## Sources

- moby/buildkit `docs/rootless.md` and `examples/kubernetes/*.rootless.yaml`
- Amazon ECS task definition parameters for Fargate (`linuxParameters`, `devices`)
- aws/containers-roadmap#1957: rootless Podman on Fargate fails with `cannot clone: Operation not permitted`
- awslabs/cfn-ecr-aws-soci-index-builder @ `770e9a5` (`utils/registry/registry.go`)
- CloudWatch `/aws/fargate/dev-app-deploy-service-app-deployer-use1` (read-only, `pennsieve-non-prod`)
