package handler

import (
	"context"
	"errors"
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
