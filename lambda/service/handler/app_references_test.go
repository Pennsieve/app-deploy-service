package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/mappers"
)

// rnaseqQuant is rnaseq-quant's app.yml: its index input is filled from the
// reference catalog, human or mouse.
const rnaseqQuant = `schemaVersion: "1.0"
application:
  name: rnaseq-quant
  type: processor
runtime:
  computeTypes: [standard]
parameters:
  - name: THREADS
    type: integer
    defaultValue: 0
inputs:
  - name: reads
    placement: subfolder
  - name: index
    description: Salmon index of the genome the reads come from.
    placement: subfolder
    reference:
      kind: genome
      assetClass: salmon-index
      toolVersion: ">=2 <3"
      choice: user
      organisms: [9606, 10090]
      default:
        taxonomyId: 9606
        assembly: GCA_000001405.29
        annotation: {provider: GENCODE, version: "50"}
      source: https://github.com/pennsieve-apps/rnaseq-index
      license: LicenseRef-GENCODE
`

func TestReferencesFollowTheSchema(t *testing.T) {
	if ps := manifestProblems([]byte(rnaseqQuant)); len(ps) != 0 {
		t.Fatalf("rnaseq-quant's reference input: %v", ps)
	}
	bad := map[string]string{
		"unknown kind":       strings.Replace(rnaseqQuant, "kind: genome", "kind: model", 1),
		"bad toolVersion":    strings.Replace(rnaseqQuant, `">=2 <3"`, `"~2"`, 1),
		"bad assembly":       strings.Replace(rnaseqQuant, "assembly: GCA_000001405.29", "assembly: hg38", 1),
		"unknown field":      strings.Replace(rnaseqQuant, "      license:", "      genomeBuild: hg38\n      license:", 1),
		"pinned, no build":   strings.Replace(rnaseqQuant, "choice: user", "choice: app", 1),
		"old top-level list": rnaseqQuant + "references:\n  - name: SALMON_INDEX\n",
	}
	for name, yml := range bad {
		if ps := manifestProblems([]byte(yml)); len(ps) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReferenceProblems(t *testing.T) {
	cases := map[string]struct {
		change string
		with   string
		want   string
	}{
		"not in its own folder": {"description: Salmon index of the genome the reads come from.\n    placement: subfolder", "description: Salmon index of the genome the reads come from.\n    placement: root", "placement: subfolder"},
		"default not offered":   {"organisms: [9606, 10090]", "organisms: [10090]", "not one of the reference's organisms"},
		"lambda only":           {"computeTypes: [standard]", "computeTypes: [lambda]", "only on lambda"},
		"taxonomy on user":      {"      organisms:", "      taxonomyId: 9606\n      organisms:", "lists the organisms"},
	}
	for name, c := range cases {
		yml := strings.Replace(rnaseqQuant, c.change, c.with, 1)
		if yml == rnaseqQuant {
			t.Fatalf("%s: the change didn't apply", name)
		}
		cfg, err := parseAppConfig([]byte(yml))
		if err != nil {
			t.Fatal(err)
		}
		ps := referenceProblems(cfg)
		if len(ps) != 1 || !strings.Contains(ps[0], c.want) {
			t.Errorf("%s: %v", name, ps)
		}
	}
	cfg, _ := parseAppConfig([]byte(rnaseqQuant + "outputs:\n  - name: counts\n    reference: {kind: genome, assetClass: salmon-index, choice: user}\n"))
	if ps := referenceProblems(cfg); len(ps) != 1 || !strings.Contains(ps[0], "only inputs") {
		t.Errorf("output reference: %v", ps)
	}
	cfg, _ = parseAppConfig([]byte(rnaseqQuant + "variants:\n  - id: small\n    name: Small\n    runtime:\n      computeTypes: [lambda]\n"))
	if ps := referenceProblems(cfg); len(ps) != 1 || !strings.Contains(ps[0], "/variants/0/runtime/computeTypes") {
		t.Errorf("lambda variant: %v", ps)
	}
	cfg, _ = parseAppConfig([]byte(strings.Replace(rnaseqQuant, "computeTypes: [standard]", "computeTypes: [lambda]", 1)[:strings.Index(rnaseqQuant, "  - name: index")]))
	if ps := referenceProblems(cfg); ps != nil {
		t.Errorf("no reference inputs: %v", ps)
	}
}

// fakeCatalog holds the queries that have matches.
type fakeCatalog struct {
	matches func(q url.Values) int
	err     error
	asked   []url.Values
}

func (f *fakeCatalog) Count(_ context.Context, q url.Values) (int, error) {
	f.asked = append(f.asked, q)
	if f.err != nil {
		return 0, f.err
	}
	return f.matches(q), nil
}

func TestReferenceCatalogProblems(t *testing.T) {
	cfg, err := parseAppConfig([]byte(rnaseqQuant))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	both := &fakeCatalog{matches: func(url.Values) int { return 1 }}
	if ps := referenceCatalogProblems(ctx, both, cfg); len(ps) != 0 {
		t.Errorf("both organisms in the catalog: %v", ps)
	}
	// One query per organism, then the default; each carries the class and range.
	if len(both.asked) != 3 || both.asked[0].Get("taxonomyId") != "9606" || both.asked[1].Get("taxonomyId") != "10090" ||
		both.asked[2].Get("annotationVersion") != "50" || both.asked[0].Get("toolVersion") != ">=2 <3" || both.asked[0].Get("assetClass") != "salmon-index" {
		t.Errorf("queries = %v", both.asked)
	}

	noMouse := &fakeCatalog{matches: func(q url.Values) int {
		if q.Get("taxonomyId") == "10090" {
			return 0
		}
		return 1
	}}
	if ps := referenceCatalogProblems(ctx, noMouse, cfg); len(ps) != 1 || !strings.Contains(ps[0], "/inputs/1/reference/organisms/1") || !strings.Contains(ps[0], "organism 10090") {
		t.Errorf("no mouse index: %v", ps)
	}

	down := &fakeCatalog{err: errors.New("connection refused")}
	if ps := referenceCatalogProblems(ctx, down, cfg); len(ps) != 0 {
		t.Errorf("an unreachable catalog was held against the app: %v", ps)
	}
	if ps := referenceCatalogProblems(ctx, nil, cfg); ps != nil {
		t.Errorf("no catalog configured: %v", ps)
	}

	refuses := &fakeCatalog{err: catalogRefusal("toolVersion: bad")}
	if ps := referenceCatalogProblems(ctx, refuses, cfg); len(ps) != 1 || !strings.Contains(ps[0], "refused") {
		t.Errorf("refused query: %v", ps)
	}
}

func TestHTTPCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/environments/catalog" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected", http.StatusTeapot)
			return
		}
		switch r.URL.Query().Get("toolVersion") {
		case "~2":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"toolVersion: \"~2\" isn't a version comparison"}`))
		default:
			_, _ = w.Write([]byte(`{"genomes":[{"name":"grch38-gencode50-salmon"}]}`))
		}
	}))
	defer srv.Close()
	cat := httpCatalog{BaseURL: srv.URL + "/environments", Client: srv.Client()}

	n, err := cat.Count(context.Background(), url.Values{"kind": {"genome"}, "toolVersion": {">=2 <3"}})
	if err != nil || n != 1 {
		t.Errorf("Count = %d, %v", n, err)
	}
	_, err = cat.Count(context.Background(), url.Values{"kind": {"genome"}, "toolVersion": {"~2"}})
	if _, ok := err.(catalogRefusal); !ok {
		t.Errorf("bad query: %v", err)
	}
}

func TestStoredManifestKeepsReferences(t *testing.T) {
	cfg, err := parseAppConfig([]byte(rnaseqQuant + "variants:\n  - id: big\n    name: Big\n    default: true\n    runtime:\n      memory: 32768\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := resolveVariants(cfg).versionManifest()
	if len(m.Inputs) != 2 || m.Inputs[0].Reference != nil || m.Inputs[1].Reference == nil {
		t.Fatalf("stored inputs = %+v", m.Inputs)
	}
	r := m.Inputs[1].Reference
	if r.AssetClass != "salmon-index" || r.ToolVersion != ">=2 <3" || len(r.Organisms) != 2 || r.Default == nil ||
		r.Default.Annotation == nil || r.Default.Annotation.Version != "50" || m.Variant == nil || m.Variant.ID != "big" {
		t.Errorf("stored reference = %+v (variant %+v)", r, m.Variant)
	}
	api := mappers.AppManifestToModel(m)
	if ref := api.Inputs[1].Reference; ref == nil || ref.Default.Assembly != "GCA_000001405.29" || ref.Choice != "user" {
		t.Errorf("API reference = %+v", ref)
	}
}

// antsMni is processor-ants-mni's app.yml: its template input is the MNI152
// brain template, pinned by its properties.
const antsMni = `schemaVersion: "1.0"
application:
  name: processor-ants-mni
  type: processor
runtime:
  computeTypes: [standard]
inputs:
  - name: t1w
    placement: subfolder
  - name: template
    placement: subfolder
    reference:
      kind: template
      assetClass: brain-template
      choice: app
      properties: {space: MNI152NLin2009cAsym, resolution: "1", desc: brain}
      source: https://www.templateflow.org
      license: LicenseRef-MNI
`

// A template reference: pinned by its properties, no genome fields; the
// fewest fields an author writes are kind and assetClass.
func TestTemplateReferencesFollowTheSchema(t *testing.T) {
	if ps := manifestProblems([]byte(antsMni)); len(ps) != 0 {
		t.Fatalf("processor-ants-mni's template input: %v", ps)
	}
	minimal := strings.Replace(antsMni, "      choice: app\n      properties: {space: MNI152NLin2009cAsym, resolution: \"1\", desc: brain}\n", "", 1)
	if ps := manifestProblems([]byte(minimal)); len(ps) != 0 {
		t.Errorf("kind and assetClass only: %v", ps)
	}
	props := "      properties: {space: MNI152NLin2009cAsym, resolution: \"1\", desc: brain}\n"
	bad := map[string]string{
		"pinned, no properties":    strings.Replace(antsMni, props, "", 1),
		"pinned, empty properties": strings.Replace(antsMni, props, "      properties: {}\n", 1),
		"bad property name":        strings.Replace(antsMni, "desc: brain", "1desc: brain", 1),
		"empty property":           strings.Replace(antsMni, "desc: brain", `desc: ""`, 1),
		"genome fields":            strings.Replace(antsMni, props, props+"      taxonomyId: 9606\n", 1),
		"genome properties":        strings.Replace(rnaseqQuant, "      license:", "      properties: {x: y}\n      license:", 1),
	}
	for name, yml := range bad {
		ps := manifestProblems([]byte(yml))
		if len(ps) == 0 || !strings.Contains(strings.Join(ps, " "), "/reference") {
			t.Errorf("%s: %v", name, ps)
		}
	}
}

// A genome without choice is a run's choice (user): no longer forced to pin.
func TestGenomeWithoutChoiceIsUser(t *testing.T) {
	yml := strings.Replace(rnaseqQuant, "      choice: user\n", "", 1)
	if ps := manifestProblems([]byte(yml)); len(ps) != 0 {
		t.Fatalf("no choice: %v", ps)
	}
	cfg, _ := parseAppConfig([]byte(yml))
	if cfg.Inputs[1].Reference.Choice != "user" {
		t.Errorf("choice = %q", cfg.Inputs[1].Reference.Choice)
	}
}

// The checks JSON Schema can't make, which also hold in warn mode: property
// limits (a manifest out of them is neither checked against the catalog nor
// stored with them), and a default contradicting the reference.
func TestReferencePropertyProblems(t *testing.T) {
	many := map[string]string{}
	for i := 0; i < 21; i++ {
		many[fmt.Sprintf("p%d", i)] = "x"
	}
	if ps := propertyProblems("/p", many); len(ps) != 1 || !strings.Contains(ps[0], "at most 20") {
		t.Errorf("21 properties: %v", ps)
	}
	if ps := propertyProblems("/p", map[string]string{"desc": strings.Repeat("x", 201)}); len(ps) != 1 {
		t.Errorf("long value: %v", ps)
	}

	cfg, _ := parseAppConfig([]byte(strings.Replace(antsMni, "desc: brain", "desc: \""+strings.Repeat("x", 300)+"\"", 1)))
	asked := 0
	cat := &fakeCatalog{matches: func(url.Values) int { asked++; return 1 }}
	if ps := referenceProblems(cfg); len(ps) != 1 || !strings.Contains(ps[0], "/inputs/1/reference/properties/desc") {
		t.Errorf("problems = %v", ps)
	}
	if referenceCatalogProblems(context.Background(), cat, cfg); asked != 0 {
		t.Errorf("an invalid reference was sent to the catalog")
	}
	if r := resolveVariants(cfg).versionManifest().Inputs[1].Reference; r.Properties != nil {
		t.Errorf("stored invalid properties: %v", r.Properties)
	}

	withDefault := strings.Replace(antsMni, "      choice: app\n", "      default: {properties: {space: MNI152NLin6Asym}}\n", 1)
	cfg, _ = parseAppConfig([]byte(withDefault))
	if ps := referenceProblems(cfg); len(ps) != 1 || !strings.Contains(ps[0], "contradicts") {
		t.Errorf("contradicting default: %v", ps)
	}
}

func TestTemplateReferenceQueryAndStore(t *testing.T) {
	cfg, err := parseAppConfig([]byte(antsMni))
	if err != nil {
		t.Fatal(err)
	}
	var asked []url.Values
	cat := &fakeCatalog{matches: func(q url.Values) int { asked = append(asked, q); return 1 }}
	if ps := referenceCatalogProblems(context.Background(), cat, cfg); len(ps) != 0 {
		t.Fatalf("problems = %v", ps)
	}
	if len(asked) != 1 || asked[0].Get("kind") != "template" || asked[0].Get("property.space") != "MNI152NLin2009cAsym" ||
		asked[0].Get("property.resolution") != "1" || asked[0].Get("taxonomyId") != "" {
		t.Errorf("asked = %v", asked)
	}
	none := &fakeCatalog{matches: func(url.Values) int { return 0 }}
	ps := referenceCatalogProblems(context.Background(), none, cfg)
	if len(ps) != 1 || !strings.Contains(ps[0], "space=MNI152NLin2009cAsym") {
		t.Errorf("no match: %v", ps)
	}

	m := resolveVariants(cfg).versionManifest()
	r := m.Inputs[1].Reference
	if r == nil || r.Kind != "template" || r.Properties["desc"] != "brain" || r.Choice != "app" {
		t.Fatalf("stored reference = %+v", r)
	}
	if ref := mappers.AppManifestToModel(m).Inputs[1].Reference; ref == nil || ref.Properties["space"] != "MNI152NLin2009cAsym" {
		t.Errorf("API reference = %+v", ref)
	}

	// No choice: a run picks (user), stored as such.
	cfg, _ = parseAppConfig([]byte(strings.Replace(antsMni, "      choice: app\n", "", 1)))
	if r := resolveVariants(cfg).versionManifest().Inputs[1].Reference; r.Choice != "user" {
		t.Errorf("default choice = %q", r.Choice)
	}
}

// The catalog's references list, for every kind.
func TestHTTPCatalogReadsReferences(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"references":[{"name":"a"},{"name":"b"}]}`))
	}))
	defer srv.Close()
	n, err := httpCatalog{BaseURL: srv.URL, Client: srv.Client()}.Count(context.Background(), url.Values{"kind": {"template"}})
	if err != nil || n != 2 {
		t.Errorf("Count = %d, %v", n, err)
	}
}
