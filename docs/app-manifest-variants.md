# Variants and build settings in app.yml

One repository and one release can produce several images of the same app, called
**variants**: for example a CPU build and a GPU build. Each variant is the app.yml's top
level with a few overrides. Both `variants` and `build` are optional; an app.yml without
them is read and stored exactly as before.

The design (identity, builds, the registry API, workflows and the UI) is in
workflow-service `docs/app-variants.md`. This page covers what app.yml may say and what
registration stores. Every variant is built on every release, each with its own image and
build status ([builds.md](builds.md#variants)), and the registry takes a variant (below).

## Example

```yaml
# yaml-language-server: $schema=https://schemas.pennsieve.io/app-manifest/v1.json
application:
  name: Seizure Onset (DynaSD)
runtime:            # shared by all variants unless a variant overrides a field
  cpu: 4096
  memory: 8192
  timeoutSeconds: 14400
build:              # shared build settings (also usable without variants)
  args:
    DYNASD_COMMIT: 48b2b959605a37fb03a4d99ae6fcc1459c04cc37
parameters:
  - name: MODEL
    defaultValue: lindda
    validValues: [lindda, ndd, absslp, oncet]
inputs:  [ ... ]
outputs: [ ... ]
variants:
  - id: cpu
    name: CPU
    default: true
    build:   { args: { TORCH_FLAVOUR: cpu } }
    runtime: { computeTypes: [standard] }
  - id: gpu
    name: GPU (ONCET)
    description: CUDA PyTorch on an NVIDIA T4; runs the pretrained ONCET model by default.
    build:   { args: { TORCH_FLAVOUR: cuda } }
    runtime: { computeTypes: [gpu], memory: 14336 }
    parameters:
      MODEL: { defaultValue: oncet }
```

## `build`

At the top level, and on each variant.

| Field | What it is |
|---|---|
| `args` | Docker build arguments (`ARG`), a map from a name (`^[A-Za-z_][A-Za-z0-9_]*$`) to a string, number or boolean. |
| `target` | The Dockerfile stage to build. |
| `dockerfile` | The Dockerfile's path, relative to the repository root and without `..`. Default: `Dockerfile`. |

A variant's build is merged over the top level's: its `args` over the top-level `args` (by
name), and its `target` and `dockerfile` in place of the top level's.

## `variants`

A list of 1 to 8 variants. The order is the display order.

| Field | What it is |
|---|---|
| `id` (required) | `^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`, with no double dashes; unique in the file; `default` is reserved. Workflows refer to a variant by its id, so keep it stable across releases. |
| `default` | This is the default variant. At most one variant sets it; without one, the first is the default. |
| `name` | A short label ("GPU (ONCET)"). Default: the id. |
| `description` | When to pick this variant, in a sentence. It doesn't replace the app's description. |
| `build` | See above. |
| `runtime` | Any of `cpu`, `memory`, `computeTypes`, `timeoutSeconds` and `llm`. Each replaces the top-level value (no deep merge: `computeTypes: [gpu]` replaces the whole list). |
| `parameters` | A map from the name of a top-level parameter to overrides of its `defaultValue`, `validValues`, `required`, `label` and `description`. |

A variant can't change the inputs, outputs, the set of parameters (it can't add, remove or
retype one), `interactive`, `sessionType`, `kernel`, `application` or `tags`; those are
always the top level's. Switching a workflow step between variants therefore never breaks
its links.

### Resolution

A variant's **resolved manifest** is the top-level manifest with the variant's runtime
fields replaced and its parameter overrides applied, plus
`variant: {id, name, description}`. `application`, `inputs`, `outputs` and `tags` come
from the top level.

### Checks

The schema checks the shape: id pattern and reserved id, at most one `default: true`,
1 to 8 variants, only the allowed fields, build arg names and scalar values, and a relative
`dockerfile` without `..`. Registration also checks:

- variant ids are unique;
- each key of a variant's `parameters` is a top-level parameter;
- a parameter's resulting `defaultValue` (overridden, or inherited when the variant
  overrides `validValues`) is one of its `validValues` and within its `min` and `max`.

These are manifest problems, handled like the schema's (`APP_MANIFEST_VALIDATION`): recorded
on the version in `warn` mode, the release refused in `enforce` mode. In `warn` mode,
variants with an invalid, reserved or repeated id are left out, and only the first 8 are
kept.

One more rule applies in either mode: **a release tag of an app with variants must not
contain `--`**, because `--` separates the tag from the variant id in image tags
(`<hash>-<tag>--<id>`). Such a release is refused with 422.

## What a version stores

For an app.yml **without** variants nothing changes: `manifest` is what it always was, and
the app's `params` are its parameters. A top-level `build`, if any, is kept as `build` on
the version.

For an app.yml **with** variants, the version (appstore-versions) gets:

```
manifest:        <resolved manifest of the DEFAULT variant>  # what variant-unaware readers see
defaultVariant:  "cpu"
variantOrder:    ["cpu", "gpu"]
variants:                                                     # a map keyed by id
  cpu: { name, description, manifest: <resolved>, build: {args, target, dockerfile},
         deploymentId, buildStorageGiB }
  gpu: { ..., deploymentId, buildStorageGiB, status: registering }
```

and the app's `params` (from its newest version) are the default variant's. Every reader
that doesn't know about variants (the app's version manifest, workflow-service, the
backfill) sees the default.

Each built variant has a deployment: the default variant's is the release's own, the others
get their own (deployment records with `variant`). Non-default variants start as
`registering`; the default variant's status is the version's until its build ends. As the
builds run, each variant gets its `destinationUrl`, `status` (`deploying`, then `deployed`
or `error`) and, on a failure, `failureReason` ([builds.md](builds.md#variants)).
`buildStorageGiB` (100 for a variant whose `computeTypes` include `gpu`) is for the
provisioner and never shown.

`backfill-manifests` resolves variants the same way.

## The API

`GET /applications/store/{id}` and `GET /applications/store` list each version with
`defaultVariant` and `variants: [{id, name, description, default, status, failureReason,
manifest}]` in app.yml order. Neither is present for a version without variants. A
variant's `status` is its own build's; the default variant's falls back to the version's,
and another variant without a recorded build has none. Deployments carry `variant` once
the builds record it.

`GET /applications/store/registry?sourceUrl=…&version=…&variant=…` (what compute nodes
call) takes an optional `variant`:

| Request | Result |
|---|---|
| no `variant` (or empty) | the version's image and status, which are the default variant's: unchanged for every app |
| `variant` on a version without variants | 404 `version <tag> has no variants` |
| a `variant` the version doesn't have | 404 `variant <id> not found for this version` |
| a variant whose build failed (`error`) | 404 `variant <id> of <tag> failed to build` |
| a variant not deployed, or a non-default one with no recorded status | 404 `variant <id> of <tag> is not yet deployed` |
| a deployed variant | its image: `<hash>-<tag>` for the default, `<hash>-<tag>--<id>` for the others (its own `destinationUrl` when recorded), SOCI-resolved as before |

Archiving and access are checked on the application, as before.
