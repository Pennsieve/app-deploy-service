# References in app.yml

Some apps need shared, published data besides their inputs: a genome index, for example. app.yml lists these as **references**. Each one is described by public identifiers (organism, assembly, annotation release, what the asset is, which tool versions can read it) and where to get it, so the app isn't tied to Pennsieve. `references` is optional; an app.yml without it is read and stored exactly as before.

The design (the catalog, delivery to steps, standards) is in the design note [References in environments-service](https://claude.ai/artifact/Ak8u7r6H3tyxvuZQJDPMGk) and environments-service `docs/design.md`.

## Example

rnaseq-quant reads a salmon index of the genome its reads come from. A run picks human or mouse:

```yaml
references:
  - name: SALMON_INDEX              # the variable the processor reads: a folder path
    description: Salmon index of the genome the reads come from.
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
references:
  - name: GENOME_INDEX
    kind: genome
    assetClass: salmon-index
    toolVersion: "2.8.0"
    choice: app
    taxonomyId: 9606
    assembly: GCA_000001405.29      # or seqcol: <GA4GH sequence-collection digest>
    annotation: {provider: GENCODE, version: "50"}
```

## Fields

| Field | Meaning |
|---|---|
| `name` | The environment variable the processor reads, holding the reference's folder path. Upper case, unique, and not a parameter's name or `INPUT_DIR`, `OUTPUT_DIR`, `REFERENCES_DIR`. |
| `kind` | `genome`. |
| `assetClass` | What the reference is; it names the files it holds. `salmon-index`: `salmon/`, `tx2gene.csv`, `genes.csv`, `index.json`. |
| `toolVersion` | For an index: the tool versions the app can read, as space-separated comparisons that must all hold: `>=2 <3`, `>=2.7.10a <2.8`, `2.8.0`. |
| `choice` | `app`: the app pins one reference, with `assembly` or `seqcol`, and `annotation` (and usually `taxonomyId`). `user`: a run picks among the matching references, optionally of `organisms`, with an optional `default`. |
| `source`, `license`, `sha256` | Where to get it outside Pennsieve, its license (SPDX), and for a pinned reference the sha256 of one exact build. |

## Running the app

The processor reads the folder from its variable, and nothing in the image knows where it came from:

- **On Pennsieve:** the reference resolves to an official reference in environments-service's catalog (`GET /environments/catalog`). The step downloads it to its own disk and sets the variable.
- **Elsewhere, or locally:** mount a copy and set the variable, such as `docker run -e SALMON_INDEX=/ref -v $PWD/grch38-salmon:/ref …`.

References are delivered to `standard` and `gpu` steps, so an app with references can't run only on `lambda` (nor can any of its variants).

## What registration checks

With the schema's other checks, and handled the same way (`APP_MANIFEST_VALIDATION`: recorded on the version in `warn` mode, the release refused in `enforce` mode):

- the entries follow the schema;
- names are unique and aren't a parameter's or the platform's;
- a `choice: user` default is one of its `organisms`;
- the app and each variant run somewhere other than only `lambda`;
- **the catalog has a match:** for a pinned reference, the reference; for `choice: user`, one per organism offered (or any, without `organisms`) and the default. The catalog does the matching, tool-version ranges included, so registration and runs agree. If the catalog can't be reached, this check is skipped and logged, not held against the app.
