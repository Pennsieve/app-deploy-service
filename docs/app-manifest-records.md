# Metadata records as an app input

A dataset on Pennsieve has files and metadata records: subjects, samples,
visits, whatever models the dataset defines, and links between them. An app
that reads records says so with an input of the structure
**`pennsieve-records`**. A run fills that input with the dataset's records; an
app without one never sees them.

## Example

An app that reads EDF recordings and the subjects they belong to:

```yaml
inputs:
  - name: recordings
    mediaTypes: [application/x-edf]
  - name: records
    description: The dataset's subjects and their recordings' samples.
    conformsTo: pennsieve-records     # metadata records, laid out as below
    placement: subfolder              # required: the records go in INPUT_DIR/records
```

The step's `INPUT_DIR`, with `sub-01/run-01.edf` and `sub-02/run-01.edf`
picked (`recordings` has no placement, so its files are at the root):

```
sub-01/run-01.edf
sub-02/run-01.edf
.pennsieve/view.json                 the dataset view they came from
records/
  models/subject/schema.json
  models/subject/records.jsonl
  models/sample/schema.json
  models/sample/records.jsonl
  relationships.csv
  files.csv                          records of a published dataset only
  .pennsieve/view.json
```

## The layout

The structure's specification is
[pennsieve-records 1.0](https://schemas.pennsieve.io/structures/pennsieve-records/v1.0)
(packages-service's format registry). In short:

- `models/<model>/schema.json`: the model's JSON Schema, with Pennsieve
  annotations such as `x-pennsieve-key`, `x-pennsieve-unit` and
  `x-pennsieve-sensitive`.
- `models/<model>/records.jsonl`: one record per line,
  `{"id": "<record id>", "data": {<field>: <value>, …}}`.
- `relationships.csv`: `source_record_id,target_record_id,relationship_type`.
  It may name records that aren't in the folder (a published dataset's table
  covers the whole dataset): join on the ids, and don't assume both ends of a
  link are present.
- `files.csv`, for records from a published dataset only:
  `record_id,package_id,path`, where `path` is the file's path in the
  published dataset (it starts with `files/`), or empty when the file isn't
  in the publication. Files reach a files input without that prefix: match on
  `package_id`.

It is Pennsieve's metadata export layout (what a Discover download has under
`metadata/`) without the `versions/<n>/` level: a run pins one version of each
model, so there is only one. Code that reads one reads the other: the same
files, one level shallower. A run brings only these: `models/…`,
`relationships.csv` and `files.csv`, plus `.pennsieve/view.json`. Other
files of the export, such as a published dataset's `codebook.json`, are left
out; a Discover download has them, and readers ignore files they don't know.

The specification has a DuckDB example that reads every model into a view
of its fields, empty files included.

Model names belong to the dataset. An app that needs particular models should
say which in its description, and fail with a clear message when they're
missing.

## Rules

An input is a records input when its first `conformsTo` entry is
`pennsieve-records` (or `pennsieve-records@1.0`); a structure nested inside
another isn't. For such ports:

- **Inputs only.** An output can't declare `pennsieve-records` yet.
- **Its own folder.** `placement: subfolder` is required.

Registration refuses a release that breaks these, whatever the manifest
validation mode. Other manifest problems only refuse a release in enforce
mode, so existing apps keep registering; records inputs are new, so no
existing app is refused by these rules.

## Which records

A run's records come from a dataset view, the same view its files come from:
the models and records the view pins, as of when the view was made.
Pennsieve materializes them for the run; they aren't copied into the app's
outputs unless the app writes them there. One dataset can feed both inputs of
the example: each link takes its part of the view, the files or the records.

## Files from a dataset

An input of files fed from a dataset view (any input that isn't
`pennsieve-records`) gets the picked files at its placement, as the app.yml
says, with the view's manifest beside them in `.pennsieve/view.json`. There is
no `files/` or `metadata/` folder: the records go only to a records input.

## Testing locally

Lay the input out yourself: `INPUT_DIR/records/models/<model>/schema.json` and
`records.jsonl`, and `INPUT_DIR/records/relationships.csv`. A Discover
download's `metadata/` folder gives real ones: move each
`models/<model>/versions/<n>/` up to `models/<model>/`.

## Review

A review run fills a records input like any other: commit a small records
folder under `tests/` and list it (`review.inputs.records.files` with
`filePaths: source`), or write one from the fixture generator
(`review.generate`, into `$REVIEW_INPUT_DIR/records/`). See
[app-manifest-review.md](app-manifest-review.md).
