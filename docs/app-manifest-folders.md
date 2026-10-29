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

### `placement: root | subfolder` (inputs)

Where an input's data is placed in `INPUT_DIR`.

- `root` (the default): at the root of `INPUT_DIR`. With one input, that's
  today's behaviour: `$INPUT_DIR` is the data, and with `conformsTo: bids`
  it is the BIDS root.
- `subfolder`: in its own folder, `INPUT_DIR/<name>/`. Use it when inputs
  would otherwise mix, e.g. two BIDS trees, each with a
  `dataset_description.json` at its root. The name is then a folder name:
  lowercase letters, numbers and dashes.
- Several inputs can share the root when the app can tell them apart by
  media type (PDFs and Open Ephys recordings side by side). They need their
  own folders when they'd mix: two inputs with the same media type, an input
  that takes any file, or a layout (a BIDS tree fills the root).

Names are identifiers either way (`study`, `recordings`): lowercase letters,
numbers and dashes keep them usable as folder names later.

Locally it's the same: set `INPUT_DIR` to a folder laid out as the app.yml
says, e.g. a BIDS tree at its root and `recordings/` beside it. Nothing
Pennsieve-specific is in the paths.

Elsewhere: Flywheel gears and DNAnexus apps always give each input its own
folder (`/flywheel/v0/input/<name>/`, `~/in/<name>/`); CWL and WDL pass each
input's path on the command line. `placement` keeps today's root as the
default and offers the Flywheel-style folder when an app needs it.

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

# Two BIDS studies: each in its own folder, INPUT_DIR/patients/ and
# INPUT_DIR/controls/.
inputs:
  - name: patients
    conformsTo: bids
    placement: subfolder
  - name: controls
    conformsTo: bids
    placement: subfolder

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

A step has one `INPUT_DIR` and one `OUTPUT_DIR`. With one input, `INPUT_DIR`
is that input's data; with several, they're merged side by side, unless
`placement: subfolder` puts one in its own folder.

- **Inputs:**
  - files keep their paths below the picked folder;
  - with `container: directory`, each folder-format item is a folder at its
    place in that tree;
  - with `conformsTo`, the tree under the picked folder is the structure.
- **Outputs:**
  - with **one** output, it is the whole of `OUTPUT_DIR`;
  - with **two or more**, each is written to its own folder,
    `OUTPUT_DIR/<name>/`, and its name must be folder-safe. In a workflow,
    each output then has its own connection point, and a link from it to a
    data target sends only that output there (the target's path prefix is
    set to the output's folder);
  - paths are kept;
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

- linking an output port to an app's input port (today an output port
  connects to a data target; apps connect through a step's single point);

- the runner unpacking folder-format inputs and packing folder outputs
  (`container`), and handing a BIDS app the `files/` folder of a published
  dataset;
- how folder formats are uploaded and stored;
- running validators (bids-validator) on structured inputs and outputs;
- `placement: subfolder` (the merge step placing an input in its own folder)
  and the run dialog asking for data per input;
- matching views to inputs through their RO-Crate `conformsTo`.
