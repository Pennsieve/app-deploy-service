# App Deploy Service

The App Store backend. It turns a GitHub release into a runnable App Store
version. For each release it:

- reads the repository's `app.yml` and `CITATION.cff`;
- builds the image(s) into the platform's private registry;
- records the version's status;
- tells compute nodes which image to run for an app, tag and variant.

It also keeps the older per-workspace applications (`/v1`, `/deploy`), which
deploy an app straight to a compute node.

## How a release becomes a version

```
GitHub release ─► POST /store (service lambda)
                    │  read app.yml (schema check), CITATION.cff, LICENSE
                    │  record the version: "registering"
                    ▼
                provisioner task (fargate/app-provisioner, ADD_TO_APPSTORE)
                    ├─ kaniko:    ECS RunTask (kaniko executor)
                    └─ codebuild: CodeBuild StartBuild (BuildKit)
                    ▼
                image pushed to <env>-appstore-private-<region>:<hash>-<tag>
                    ▼
                status lambda (ECS / CodeBuild state changes) ─► "deployed" | "error"
                    ▼
                SOCI index builder adds <hash>-<tag>-soci
```

Compute nodes ask `GET /store/registry` for the image of an app version and
variant. Workflow-service reads the versions table for manifests.

## What a version records

| From | What | Docs |
|---|---|---|
| `app.yml` | name, description, runtime (cpu, memory, compute types, GPU, storage, LLM opt-in), parameters, input/output ports and folders | [app-manifest-folders.md](docs/app-manifest-folders.md); the JSON Schema is published to `schemas.<domain>/app-manifest/v1.json` |
| `app.yml` `variants` / `build` | several images from one release (e.g. CPU and GPU), with build args, target and Dockerfile | [app-manifest-variants.md](docs/app-manifest-variants.md) |
| `CITATION.cff` | authors (with ORCID), preferred citation, related works typed for DataCite; `application.relations` in app.yml overrides types | parsed with pennsieve-go-core `pkg/citation` |
| `LICENSE` | SPDX id, as GitHub reads it | |

Problems in app.yml or CITATION.cff are recorded on the version
(`manifestProblems`). `app_manifest_validation` decides what happens to them:
- `"warn"` (the default): the release is never refused.
- `"enforce"`: an app.yml that fails the schema is refused.

## Image builds

`image_builder` picks the builder per environment; see [builds.md](docs/builds.md).

| `image_builder` | Builder | Notes |
|---|---|---|
| `kaniko` (default) | kaniko Fargate task, v1.24.0 pinned | kaniko is archived. It ignores Dockerfile heredocs and `RUN --mount`, so those releases are refused. No cache. |
| `codebuild` | BuildKit v0.34.1 in a CodeBuild project | Same engine as `docker build`, with a per-app registry cache. Docker Hub is pulled with the platform's account; an ECR pull-through cache can be put in front (`image_builder_dockerhub_cache`). |

Why CodeBuild and not BuildKit on Fargate: [buildkit-spike.md](docs/buildkit-spike.md).

## API

Routes are defined in [terraform/app-deploy-service.yml](terraform/app-deploy-service.yml),
which is both the OpenAPI spec and the API Gateway definition.

| Route | What |
|---|---|
| `POST /store` | publish a release to the App Store (the GitHub release path) |
| `GET /store/me`, `GET /store/{id}` | list and read App Store applications and their versions (manifest, variants, citation, status) |
| `PATCH /store/{id}` | update an App Store application |
| `GET /store/registry` | resolve the image for an app version; `&variant=` for a non-default variant |
| `POST /store/sync` | sync an application's details |
| `GET /store/{id}/asset` | a file from an application's synced repository content, per tag |
| `GET`/`PUT /store/{id}/permissions` | who can see and run an application |
| `POST /v1`, `GET /{id}`, `POST /deploy`, `GET /{id}/deployments[/{deploymentId}]` | per-workspace applications deployed to a compute node |

[github-integration-and-app-store.md](docs/github-integration-and-app-store.md)
walks through publishing from GitHub: linking GitHub, choosing repositories,
releasing, and permissions.

## Layout

| Path | What |
|---|---|
| `lambda/service` | API lambda: handlers, app.yml and CITATION.cff reading, DynamoDB stores |
| `lambda/service/cmd/check-app-access` | lambda the realtime events authorizer calls to decide whether a user may subscribe to an application's channel |
| `lambda/service/cmd/backfill-manifests` | one-off tool: store app.yml for versions registered before versions kept theirs |
| `lambda/status` | status lambda: turns ECS task and CodeBuild state changes into deployment and version status |
| `fargate/app-provisioner` | provisioner task: starts builds (kaniko or CodeBuild) and deploys per-workspace apps |
| `fargate/buildkit-builder` | the BuildKit spike prototype (not deployed) |
| `terraform` | the service's infrastructure; `terraform/image-builder` holds CodeBuild's `build.sh` and buildspec |
| `docs` | design and how-to docs (linked above) |

The DynamoDB tables are applications, App Store applications, App Store
versions, app access, and deployments.

## Configuration

The infrastructure repo calls the module once per environment, from
`aws/<account>/<region>/<vpc>/<env>/app-deploy-service`. Notable variables:

| Variable | Default | What |
|---|---|---|
| `image_builder` | `kaniko` | `kaniko` or `codebuild` |
| `image_builder_dockerhub_auth` | `true` | CodeBuild: pull from Docker Hub with the platform's account |
| `image_builder_dockerhub_cache` | `false` | CodeBuild: use the ECR pull-through cache for Docker Hub; the rule is created outside this module (see builds.md) |
| `image_builder_compute_type`, `image_builder_timeout_minutes` | | CodeBuild size and timeout |
| `build_cache_expiry_days` | `30` | drop an app's build cache after this many days without a release |
| `app_manifest_validation` | `warn` | `warn` or `enforce` |
| `publish_schemas` | `false` | publish the app.yml JSON Schema to the schema host |
| `realtime_events_enabled` | on in dev only | publish live updates over AppSync realtime events instead of Pusher |

## Development

```sh
make test          # dockerized tests (DynamoDB Local), as Jenkins runs them
cd lambda/service && go test ./...
cd lambda/status && go test ./...
cd fargate/app-provisioner && go test ./...
```

[builds.md](docs/builds.md#testing-locally) explains how to run
`terraform/image-builder/build.sh` locally.
