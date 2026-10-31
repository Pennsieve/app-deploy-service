# Review inputs in app.yml

Before a release is made public, the platform will review it: it runs the app on test
inputs, with no network and no Pennsieve credentials, and checks that the app produces
what app.yml declares. The optional `review` block says which inputs to use and which
parameter values to set for the review run. Which outputs the run must fill comes from
the outputs themselves (below).

The review itself (where it runs, its states, the report and the gate) is designed in
"Automated app review: design"; reviews don't run yet. Registration already reads and
checks the block, and stores it on the version (`review`, with the release's commit and
each image's digest: [builds.md](builds.md#what-a-version-records-about-its-build)), so
apps can add it now.

## Example

```yaml
# yaml-language-server: $schema=https://schemas.pennsieve.io/app-manifest/v1.json
application:
  name: FCS Prep
parameters:
  - name: MAX_EVENTS_PER_SAMPLE
    type: integer
    defaultValue: 0
    min: 0
inputs:
  - name: fcs
    mediaTypes: [application/vnd.isac.fcs, text/csv]
outputs:
  - name: fcs
  - name: metadata
review:
  version: 1
  generate:
    command: python -m tests.make_fixture "$REVIEW_INPUT_DIR"
    root: linked/fcs
  parameters:
    MAX_EVENTS_PER_SAMPLE: 2000
```

## Fields

| Field | What it says |
| --- | --- |
| `version` | Required. The block's format: `1`. |
| `generate` | A command that writes the whole input directory, laid out as the app reads `INPUT_DIR`: ports with `placement: subfolder` in their own folders. Most apps' test fixture generators already write this. Fields as for `inputs.<port>.generate` below. |
| `generate.root` | The folder within what the command wrote that becomes the input directory, e.g. `linked` when a generator writes `linked/` and `flat/` layouts. Default: all of it. |
| `inputs.<port>` | Where a review run gets the data for one input port, by the port's name: exactly one of `generate`, `files` or `dataset`. A port listed here is taken from its source rather than from the top-level `generate`. Ports in neither are left to the platform (and the review may ask the author). |
| `inputs.<port>.generate` | `command`, run with `sh -c` from the repository root, writes the input into `$REVIEW_INPUT_DIR`; it must not delete or replace that directory. It runs in a platform generator image, `image: python` (the default; Python 3.12 with numpy 2, pandas, scipy, h5py, anndata, zarr, scanpy, FlowIO, edfio, nibabel, pyarrow, openpyxl) or `r` (base R), not the app's own image (which usually doesn't contain `tests/`). The repository is read-only and there is no network. A generator that needs public data names it as `source` (a `dataset`, below): the platform fetches it first and mounts it read-only at `$REVIEW_SOURCE_DIR`. |
| `inputs.<port>.files` | Files or glob patterns committed in the repository, relative to its root, without `..`. |
| `inputs.<port>.dataset` | A public dataset: `doi` (e.g. `10.5281/zenodo.15388817`) or an `https` `url`, and optionally `files` (patterns within it). The platform fetches it from an allowed host (Zenodo, Figshare, OSF, OpenNeuro), with a size limit. |
| `parameters.<name>` | A value for the review run only, e.g. a smaller sample. It must be one of the parameter's `validValues` and within its `min` and `max`. Other parameters keep their `defaultValue`. |

## Outputs a review run must fill

A review run must write files to every output port, except those marked
`required: false` on the port itself: outputs a run may leave empty, such as a report
section made only for some inputs.

```yaml
outputs:
  - name: figures
  - name: ccc_spatial
    required: false   # only when cell-cell communication inputs are given
```

## Relation to other formats

No existing test format covers what reviews need: inputs a command generates, or a public
dataset the platform fetches. So `review` is app.yml's own; like the rest of app.yml, it
borrows names where they overlap. If reviews later check more than "outputs receive
files", those checks will follow Boutiques' `tests` (`exit-code`, `output-files`). Datasets
are named by DOI, as RO-Crate and DataCite do.

## What registration checks

The JSON Schema checks the block's shape. Registration also checks what the schema can't:
every `inputs` and `parameters` name is one of the app's own ports or parameters, and parameter values fit the parameter. Problems are reported like any other
app.yml problem: recorded on the version in `warn` mode, refusing the release in `enforce`
mode (`APP_MANIFEST_VALIDATION`).

## Choosing a source

- **A generator** when the repository already makes a test fixture (`make fixture`),
  and the generator needs nothing but the repository and common Python or R packages.
  Use the top-level `generate` when it writes every port; most do.
- **Committed files** when the fixture is small (a few MB) or the generator needs more
  than the generator image has.
- **A generator with a `source`** when the fixture is a subsample of public data, as the
  cytometry apps' generators subsample the Zenodo demo set:

  ```yaml
  generate:
    command: python tests/make_fixture.py "$REVIEW_SOURCE_DIR" "$REVIEW_INPUT_DIR" --events 2000
    source: { doi: 10.5281/zenodo.15388817, files: ["*.fcs"] }
  ```
- **A dataset** when realistic input is public and small enough as it is, e.g. one subject
  of an OpenNeuro dataset. Prefer a small subset with `files`.

Name the arguments explicitly in `command` rather than calling `make fixture`: generators
differ in how they take their output directory. Give every input port `mediaTypes` (or
`formats`), so a review can check what the generator wrote.

Inputs are reviewed as given: a review that fails on them fails the app, so they should be
inputs the app is expected to handle.
