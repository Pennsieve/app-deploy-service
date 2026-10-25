# Folders in app.yml

How an app's inputs and outputs describe folders: a folder format such as
OME-Zarr, a folder structure such as BIDS, or both. Part of app-manifest v1
(`https://schemas.pennsieve.io/app-manifest/v1.json`). Both fields are
optional, so every app.yml written before them is unchanged.

## What a port can be

Each input and output is one of these, or a structure that holds files:

| Kind | Written as | Example |
|---|---|---|
| Files | `mediaTypes` and/or `formats` (EDAM) | EDF recordings, DICOM slices |
| Folder format: each item is a folder | `mediaTypes` of a format with `kind: directory`, and `container` | OME-Zarr, TS-Zarr, MEF3 |
| Folder structure: the whole port is one tree with a layout | `conformsTo` | BIDS, SPARC SDS, a published dataset |

A port with none of these takes any file, as before.

Which one to pick:

- **Each file is useful on its own** (a `.dcm` slice, an `.edf` recording):
  files. A folder of them is still files, because input paths are kept.
- **The folder is one thing whose parts mean nothing alone** (Zarr chunks, a
  `.mefd` session): a folder format.
- **Many meaningful files follow a published layout the app relies on**
  (`sub-*/ses-*/anat/…`): a structure.

## The fields

### `container: file | directory`

How folder-format items are given to the app, or written by it.

- `directory`: each item is a plain folder with the format's folder ending,
  e.g. `brain-01.ome.zarr/` or `session.mefd/`. The endings are the format
  registry's `folderExtensions`.
- `file`: each item as Pennsieve stores it, one compressed file.
- **Default:** `file`, except on a port with `conformsTo`, where it is
  `directory`. A structure is specified in its unpacked form: BIDS expects
  `.ome.zarr` folders, not zips.

It only matters for folder formats. For plain files there is no difference.

### `conformsTo`

The folder structure the whole port follows. Its value can be:

- **a registry structure id**, optionally with a version: `bids`,
  `bids@1.9`, `pennsieve-published-dataset@5.0`. These are what the
  platform understands: matching, where an inner structure starts, and
  validators.
- **a link to any other specification**: `https://example.org/lab-layout/v2`.
  It only matches the same link, and the platform does nothing more with it.
- **a list, outermost first**, to nest structures:
  `[pennsieve-published-dataset, bids]` is a published dataset whose `files/`
  folder is BIDS. Each structure but the last must have a `contentRoot` in the
  registry: that's where the next one starts.

A port with `conformsTo` is always a folder tree with its paths kept. On such
a port, `mediaTypes` say what the tree holds, and `container` how its
folder-format items are given.

The structures in the registry (`GET /packages/formats`, `structures`; also
`https://schemas.pennsieve.io/formats/v1.json`):

| id | What | contentRoot |
|---|---|---|
| `bids` | Brain Imaging Data Structure | |
| `sparc-sds` | SPARC Dataset Structure | |
| `pennsieve-published-dataset` | Pennsieve Published Dataset (PPD): `manifest.json`, `readme.md`, `changelog.md`, `banner.jpg`, `files/` | `files` |
| `ro-crate` | A folder described by `ro-crate-metadata.json` (Pennsieve views) | |

Adding one is a PR to packages-service's format registry.

## Examples

```yaml
inputs:
  # A set of OME-Zarr images, each as a folder.
  - name: images
    mediaTypes: [application/vnd.pennsieve.ome-zarr]
    container: directory

  # A BIDS study. Its OME-Zarr microscopy arrives as .ome.zarr folders,
  # because a structure's container defaults to directory.
  - name: study
    conformsTo: bids
    mediaTypes: [application/vnd.pennsieve.ome-zarr]

  # A published dataset whose files/ folder is BIDS.
  - name: published
    conformsTo: [pennsieve-published-dataset, bids]

  # DICOM: plain files.
  - name: scans
    mediaTypes: [application/dicom]

outputs:
  # Segmentations, written as .ome.zarr folders.
  - name: labels
    mediaTypes: [application/vnd.pennsieve.ome-zarr]
    container: directory

  # A BIDS derivatives tree.
  - name: derivatives
    conformsTo: bids
```

## What the app sees

A step has one `INPUT_DIR` and one `OUTPUT_DIR`. Port names are labels, not
folders.

- **Inputs:**
  - files land in `INPUT_DIR/<picked source>/<subfolders…>/<file>`, with
    paths kept;
  - with `container: directory`, each folder-format item is a folder at its
    place in that tree;
  - with `conformsTo`, the tree under the picked folder is the structure.
- **Outputs:**
  - the app writes into `OUTPUT_DIR`, and paths are kept;
  - folder-format items are written as folders with the format's folder
    ending. They're recognised by that ending, not by port, so apps that
    declare no outputs work too;
  - with `conformsTo`, the app writes the tree in that layout.

## Connecting steps in a workflow

An output can feed an input when:

- **with structures:** the input's `conformsTo` list is the end of the
  output's list (ids compared without versions; links compared exactly):

  | Output | Input | Connects |
  |---|---|---|
  | `[pennsieve-published-dataset, bids]` | `[pennsieve-published-dataset, bids]` | yes |
  | `[pennsieve-published-dataset, bids]` | `bids` | yes: the app gets `files/` |
  | `bids` | `[pennsieve-published-dataset, bids]` | no |

  and, where both list `mediaTypes`, they overlap;
- **without structures:** their media types overlap, as before.

A structure never feeds a plain-files input, and plain files never feed a
structure.

## Checking

- The schema checks the values: `container`'s two values, and that each
  `conformsTo` entry is a structure id or an `https://` link.
- The builder only offers registry structures, or a typed link.
- Registration doesn't yet check that an id is in the registry. When it
  does, it should only warn, so a newer registry doesn't break an older
  deploy.

## Not yet

These are declared in app.yml now and acted on later:

- the runner unpacking folder-format inputs and packing folder outputs
  (`container`), and handing a BIDS app the `files/` folder of a published
  dataset;
- how folder formats are uploaded and stored;
- running validators (bids-validator) on structured inputs and outputs;
- a folder per port (`INPUT_DIR/<name>/`);
- matching views to inputs through their RO-Crate `conformsTo`.
