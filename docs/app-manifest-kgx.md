# A knowledge graph as an app input

A dataset's metadata records can also reach an app as a knowledge graph: the
records are nodes, the links between them and their coded values (ontology
terms) are edges, in the tables of the
[Knowledge Graph Exchange](https://github.com/biolink/kgx) (KGX) format. It is
the input graph tools read: NetworkX, GRAPE, PyKEEN, PyG, KGTK, kgx and Neo4j
import. An app that reads it says so with an input of the structure
**`kgx`**. Biolink is used where known and isn't required.

## Example

```yaml
inputs:
  - name: graph
    description: The dataset's records as a graph.
    conformsTo: kgx          # a knowledge graph, laid out as below
    placement: subfolder     # required: the graph goes in INPUT_DIR/graph
```

The step's `INPUT_DIR`:

```
graph/
  nodes.tsv.gz         KGX TSV, gzipped (canonical)
  edges.tsv.gz
  nodes.parquet        the same rows, typed
  edges.parquet
  graph.json           provenance, prefixes, counts
  graph_stats.yaml     kgx graph-summary form
  .pennsieve/view.json the dataset view the graph came from
```

## The layout

The structure's specification is
[kgx 1.0](https://schemas.pennsieve.io/structures/kgx/v1.0)
(packages-service's format registry): the columns, ids, TSV rules and how to
load the graph in pandas, DuckDB, NetworkX, GRAPE, kgx and Neo4j. In short:

- Nodes: `id`, `category`, `name`, `provided_by`, `properties` (a JSON
  object of the record's fields). Records are `pennsieve:<record uuid>` of
  category `pennsieve:<model>`; terms are their CURIEs.
- Edges: `id`, `subject`, `predicate`, `object`, plus provenance columns and
  `pennsieve_property`. Relationships are `pennsieve:<type, lower case>`;
  coded values are `biolink:has_attribute` edges to term nodes.
- The TSV is unquoted (read it with `QUOTE_NONE`); DuckDB, Polars and Neo4j
  read the Parquet files. GRAPE reads plain TSV: gunzip first.
- Sensitive fields are in no file.

Read the core columns by name (`id`, `category`, `name`, `provided_by`;
`id`, `subject`, `predicate`, `object`), ignore columns you don't know, and
treat `properties` and the other Pennsieve columns as optional. Then KGX
graphs from elsewhere (Monarch KG, KG-Hub graphs) can feed the same app
later.

Other graph formats (RDF, Neo4j, TigerGraph) are conversions inside the app,
such as `kgx transform`; Pennsieve writes only this layout.

## Rules

An input is a graph input when its first `conformsTo` entry is `kgx` (or
`kgx@1.0`); a structure nested inside another isn't. The rules are those of
[records inputs](app-manifest-records.md):

- **Inputs only.** An output can't declare `kgx` yet.
- **Its own folder.** `placement: subfolder` is required.

Registration refuses a release that breaks these, whatever the manifest
validation mode.

## Which graph

A run's graph comes from a dataset view: the graph Pennsieve's metadata
export renders from the records the view pins, as of when the view was made.
One dataset can feed several inputs of a step, each link taking its part of
the view: the files, the records, the graph. A run is refused before it
starts when:

- the dataset has no metadata records (there is nothing to make a graph of);
- the view was made before graphs were (make the view again);
- the dataset is published: graphs aren't published yet, so pick a
  workspace dataset;
- the compute node's provisioner is older than the release that stages
  graphs (update the node).

A notebook gets the whole view, the graph under `metadata/kgx/`.

## Testing locally

Put a graph's six files in `INPUT_DIR/graph/`. Any KGX TSV pair works for
code that reads only the core columns; a view's `metadata/kgx/` folder gives
real ones.
