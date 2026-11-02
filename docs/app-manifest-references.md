# Reference inputs in app.yml

Some apps need shared, published data besides the user's files: a genome index, or a brain template, for example. app.yml marks such an input with a **`reference`**. The data is described by public identifiers (organism, assembly, annotation release, what the asset is, which tool versions can read it) and where to get it, so the app isn't tied to Pennsieve. On Pennsieve the input is filled from the reference catalog. `reference` is optional; an app.yml without it is read and stored exactly as before.

The design (the catalog, delivery to steps, standards) is in the design note [References in environments-service](https://claude.ai/artifact/Ak8u7r6H3tyxvuZQJDPMGk) and environments-service `docs/design.md`.

## Example

rnaseq-quant's `index` input is a salmon index of the genome its reads come from. A run picks human or mouse:

```yaml
inputs:
  - name: reads
    placement: subfolder
  - name: index
    description: Salmon index of the genome the reads come from.
    placement: subfolder              # the reference is unpacked into INPUT_DIR/index
    reference:
      kind: genome
      assetClass: salmon-index        # rnaseq-index's output
      toolVersion: ">=2 <3"           # the salmon versions this app can read
      choice: user                    # a run picks among the matching references
      organisms: [9606, 10090]        # human, mouse (NCBI taxonomy ids)
      default:
        taxonomyId: 9606
        assembly: GCA_000001405.29    # GRCh38
        annotation: {provider: GENCODE, version: "50"}
      source: https://github.com/pennsieve-apps/rnaseq-index
      license: LicenseRef-GENCODE
```

An app that always uses one reference pins it:

```yaml
    reference:
      kind: genome
      assetClass: salmon-index
      toolVersion: "2.8.0"
      choice: app
      taxonomyId: 9606
      assembly: GCA_000001405.29      # or seqcol: <GA4GH sequence-collection digest>
      annotation: {provider: GENCODE, version: "50"}
```

A template is pinned by its properties, TemplateFlow's terms for it (processor-ants-mni's MNI152 template):

```yaml
  - name: template
    placement: subfolder              # unpacked into INPUT_DIR/template, TemplateFlow's file names
    reference:
      kind: template
      assetClass: brain-template
      choice: app
      properties: {space: MNI152NLin2009cAsym, resolution: "1", desc: brain}
      source: https://www.templateflow.org
```

The fewest fields are `kind` and `assetClass`: a run then picks among every match, and gets the newest by default.

## Fields of `reference`

| Field | Meaning |
|---|---|
| `kind` | `genome` or `template`. |
| `assetClass` | What the reference is; it names the files it holds. `salmon-index`: `salmon/`, `tx2gene.csv`, `genes.csv`, `index.json`. |
| `toolVersion` | For an index: the tool versions the app can read, as space-separated comparisons that must all hold: `>=2 <3`, `>=2.7.10a <2.8`, `2.8.0`. |
| `choice` | `app`: the app pins one reference: a genome with `assembly` or `seqcol`, and `annotation` (and usually `taxonomyId`); a template with `properties`. `user` (the default): a run picks among the matching references, optionally of `organisms`, with an optional `default`. |
| `properties` | What every match is, in its own terms, matched exactly: a template's `space`, `resolution` and `desc`. `default` may carry its own. |
| `source`, `license`, `sha256` | Where to get it outside Pennsieve, its license (SPDX), and for a pinned reference the sha256 of one exact build. |

The input needs `placement: subfolder`: the reference is unpacked into its folder.

## Running the app

The processor reads the input's folder like any other input, and nothing in the image knows where it came from:

- **On Pennsieve:** a workflow fills the input from a **reference source**, a source node like a dataset source that names a catalog reference. The builder adds one with the input's default when you add the app; without one, a run uses the default. The step downloads the reference to its own disk and unpacks it into `INPUT_DIR/<input>`.
- **Elsewhere, or locally:** put a copy in the input's folder, such as `INPUT_DIR/index/`.

References are delivered to `standard` and `gpu` steps, so an app with reference inputs can't run only on `lambda` (nor can any of its variants).

## What registration checks

With the schema's other checks, and handled the same way (`APP_MANIFEST_VALIDATION`: recorded on the version in `warn` mode, the release refused in `enforce` mode):

- the reference follows the schema;
- only inputs have one, each with `placement: subfolder`;
- a `choice: user` default is one of its `organisms`;
- the app and each variant run somewhere other than only `lambda`;
- **the catalog has a match:** for a pinned reference, the reference; for `choice: user`, one per organism offered (or any, without `organisms`) and the default. The catalog does the matching, tool-version ranges included, so registration and runs agree. If the catalog can't be reached, this check is skipped and logged, not held against the app.
