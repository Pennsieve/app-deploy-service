package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// portReference is an input port's reference (app.yml inputs[].reference):
// shared, published data the port is filled with from Pennsieve's catalog,
// described by public identifiers so the app isn't tied to Pennsieve. A
// run's reference source fills it (the default when the workflow has none),
// unpacked into the input's folder.
type portReference struct {
	Kind        string               `yaml:"kind"`
	AssetClass  string               `yaml:"assetClass"`
	ToolVersion string               `yaml:"toolVersion"`
	Choice      string               `yaml:"choice"`
	TaxonomyID  int                  `yaml:"taxonomyId"`
	Assembly    string               `yaml:"assembly"`
	Seqcol      string               `yaml:"seqcol"`
	Annotation  *referenceAnnotation `yaml:"annotation"`
	// Properties every match has (a template's space, resolution, desc).
	Properties map[string]string  `yaml:"properties"`
	Organisms  []int              `yaml:"organisms"`
	Default    *referenceIdentity `yaml:"default"`
	Source     string             `yaml:"source"`
	SHA256     string             `yaml:"sha256"`
	License    string             `yaml:"license"`
}

// referenceAnnotation is an annotation release: GENCODE 50.
type referenceAnnotation struct {
	Provider string `yaml:"provider"`
	Version  string `yaml:"version"`
}

// referenceIdentity names one reference: a choice: user entry's default.
type referenceIdentity struct {
	TaxonomyID int                  `yaml:"taxonomyId"`
	Assembly   string               `yaml:"assembly"`
	Seqcol     string               `yaml:"seqcol"`
	Annotation *referenceAnnotation `yaml:"annotation"`
	Properties map[string]string    `yaml:"properties"`
}

// choiceOf is a reference's choice: user unless it pins one (app).
func choiceOf(r portReference) string {
	if r.Choice == "" {
		return "user"
	}
	return r.Choice
}

// referenceProblems are the checks on inputs' references that JSON Schema
// can't express: only inputs take them, each in its own folder (placement:
// subfolder), a choice: user default is one of its organisms, and the app
// (and each variant) runs somewhere references are delivered, not only on
// Lambda.
func referenceProblems(c appConfig) []string {
	var out []string
	has := false
	for i, p := range c.Inputs {
		r := p.Reference
		if r == nil {
			continue
		}
		has = true
		path := fmt.Sprintf("/inputs/%d", i)
		if p.Placement != "subfolder" {
			out = append(out, fmt.Sprintf("%s/placement: an input filled from the reference catalog needs placement: subfolder (the reference is unpacked into its folder)", path))
		}
		if choiceOf(*r) == "user" && r.Default != nil && r.Default.TaxonomyID != 0 && len(r.Organisms) > 0 && !containsInt(r.Organisms, r.Default.TaxonomyID) {
			out = append(out, fmt.Sprintf("%s/reference/default/taxonomyId: %d is not one of the reference's organisms %v", path, r.Default.TaxonomyID, r.Organisms))
		}
		if choiceOf(*r) == "user" && r.TaxonomyID != 0 {
			out = append(out, fmt.Sprintf("%s/reference/taxonomyId: a choice: user reference lists the organisms a run may pick in organisms", path))
		}
	}
	for i, p := range c.Outputs {
		if p.Reference != nil {
			out = append(out, fmt.Sprintf("/outputs/%d/reference: only inputs are filled from the reference catalog", i))
		}
	}
	if !has {
		return out
	}
	// References are delivered to ECS steps (standard, gpu), not Lambda ones.
	if lambdaOnly(defaultComputeTypes(c.Runtime.ComputeTypes)) {
		out = append(out, "/runtime/computeTypes: an app with reference inputs can't run only on lambda; references are delivered to standard and gpu steps")
	}
	for i, v := range c.Variants {
		if v.Runtime.ComputeTypes != nil && lambdaOnly(v.Runtime.ComputeTypes) {
			out = append(out, fmt.Sprintf("/variants/%d/runtime/computeTypes: an app with reference inputs can't run only on lambda; references are delivered to standard and gpu steps", i))
		}
	}
	return out
}

func lambdaOnly(ct []string) bool {
	if len(ct) == 0 {
		return false
	}
	for _, t := range ct {
		if !strings.EqualFold(strings.TrimSpace(t), "lambda") {
			return false
		}
	}
	return true
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// referenceCatalog asks environments-service's catalog which official
// references match a query (GET /catalog, public). The catalog does the
// matching, tool-version ranges included, for app review and runs alike.
type referenceCatalog interface {
	Count(ctx context.Context, query url.Values) (int, error)
}

// httpCatalog is the catalog at https://api2.<domain>/environments.
type httpCatalog struct {
	BaseURL string
	Client  *http.Client
}

// catalogFromEnv is the catalog of this environment (PENNSIEVE_DOMAIN), or
// nil when the domain isn't set.
func catalogFromEnv() referenceCatalog {
	domain := strings.TrimSpace(os.Getenv("PENNSIEVE_DOMAIN"))
	if domain == "" {
		return nil
	}
	return httpCatalog{BaseURL: "https://api2." + domain + "/environments", Client: &http.Client{Timeout: 10 * time.Second}}
}

func (c httpCatalog) Count(ctx context.Context, query url.Values) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/catalog?"+query.Encode(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var body struct {
		References []json.RawMessage `json:"references"`
		Genomes    []json.RawMessage `json:"genomes"`
		Message    string            `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("reading the catalog's answer (%d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		return 0, catalogRefusal(body.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("the catalog answered %d", resp.StatusCode)
	}
	// references: every kind; genomes: a genome catalog from before kinds.
	if body.References != nil {
		return len(body.References), nil
	}
	return len(body.Genomes), nil
}

// catalogRefusal is the catalog saying the query itself is wrong (a bad
// toolVersion): a problem with the app.yml, not with the catalog.
type catalogRefusal string

func (e catalogRefusal) Error() string { return string(e) }

// referenceQuery is the catalog query for an entry, narrowed to one identity
// (a pinned reference, a default, or one organism): the reference's
// properties, and the identity's (property.<name>=<value>).
func referenceQuery(r portReference, id referenceIdentity) url.Values {
	q := url.Values{"kind": {r.Kind}, "assetClass": {r.AssetClass}}
	if r.ToolVersion != "" {
		q.Set("toolVersion", r.ToolVersion)
	}
	for _, props := range []map[string]string{r.Properties, id.Properties} {
		for k, v := range props {
			q.Set("property."+k, v)
		}
	}
	if id.TaxonomyID != 0 {
		q.Set("taxonomyId", strconv.Itoa(id.TaxonomyID))
	}
	if id.Assembly != "" {
		q.Set("assembly", id.Assembly)
	}
	if id.Seqcol != "" {
		q.Set("seqcol", id.Seqcol)
	}
	if id.Annotation != nil {
		q.Set("annotationProvider", id.Annotation.Provider)
		q.Set("annotationVersion", id.Annotation.Version)
	}
	return q
}

// referenceCatalogProblems checks that each input's reference resolves in
// the catalog: a pinned one to a reference, a choice: user one to at least
// one per organism it offers, and its default to one. A catalog that can't
// be reached is logged and not held against the app.
func referenceCatalogProblems(ctx context.Context, cat referenceCatalog, c appConfig) []string {
	if cat == nil {
		return nil
	}
	var out []string
	for i, p := range c.Inputs {
		if p.Reference == nil {
			continue
		}
		r := *p.Reference
		path := fmt.Sprintf("/inputs/%d/reference", i)
		type check struct {
			path string
			id   referenceIdentity
			what string
		}
		var checks []check
		switch choiceOf(r) {
		case "app":
			checks = append(checks, check{path, referenceIdentity{r.TaxonomyID, r.Assembly, r.Seqcol, r.Annotation, nil}, "the pinned reference"})
		case "user":
			if len(r.Organisms) == 0 {
				checks = append(checks, check{path, referenceIdentity{}, "any reference"})
			}
			for j, taxon := range r.Organisms {
				checks = append(checks, check{fmt.Sprintf("%s/organisms/%d", path, j), referenceIdentity{TaxonomyID: taxon}, fmt.Sprintf("a reference for organism %d", taxon)})
			}
			if r.Default != nil {
				checks = append(checks, check{path + "/default", *r.Default, "the default reference"})
			}
		}
		for _, ch := range checks {
			n, err := cat.Count(ctx, referenceQuery(r, ch.id))
			if refusal, ok := err.(catalogRefusal); ok {
				out = append(out, fmt.Sprintf("%s: the catalog refused the query: %s", path, refusal))
				break
			}
			if err != nil {
				log.Printf("warning: references not checked against the catalog: %v", err)
				return out
			}
			if n == 0 {
				out = append(out, fmt.Sprintf("%s: Pennsieve's catalog has no %s %s for %s (%s)", ch.path, r.Kind, r.AssetClass, ch.what, describeQuery(r, ch.id)))
			}
		}
	}
	return out
}

// describeQuery is an entry's identity as a problem message shows it.
func describeQuery(r portReference, id referenceIdentity) string {
	var parts []string
	if id.TaxonomyID != 0 {
		parts = append(parts, "taxonomy "+strconv.Itoa(id.TaxonomyID))
	}
	if id.Assembly != "" {
		parts = append(parts, id.Assembly)
	}
	if id.Seqcol != "" {
		parts = append(parts, "seqcol "+id.Seqcol)
	}
	if id.Annotation != nil {
		parts = append(parts, id.Annotation.Provider+" "+id.Annotation.Version)
	}
	if r.ToolVersion != "" {
		parts = append(parts, "tool version "+r.ToolVersion)
	}
	q := referenceQuery(r, id)
	var props []string
	for k := range q {
		if name, ok := strings.CutPrefix(k, "property."); ok {
			props = append(props, name+"="+q.Get(k))
		}
	}
	sort.Strings(props)
	parts = append(parts, props...)
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, ", ")
}

// storedReference is an input's reference as the version records it.
func storedReference(r *portReference) *store_dynamodb.PortReference {
	if r == nil {
		return nil
	}
	annotation := func(a *referenceAnnotation) *store_dynamodb.ReferenceAnnotation {
		if a == nil {
			return nil
		}
		return &store_dynamodb.ReferenceAnnotation{Provider: a.Provider, Version: a.Version}
	}
	s := &store_dynamodb.PortReference{
		Kind: r.Kind, AssetClass: r.AssetClass, ToolVersion: r.ToolVersion, Choice: choiceOf(*r), TaxonomyID: r.TaxonomyID,
		Assembly: r.Assembly, Seqcol: r.Seqcol, Annotation: annotation(r.Annotation), Properties: r.Properties,
		Organisms: r.Organisms, Source: r.Source, SHA256: r.SHA256, License: r.License,
	}
	if r.Default != nil {
		s.Default = &store_dynamodb.ReferenceIdentity{TaxonomyID: r.Default.TaxonomyID, Assembly: r.Default.Assembly,
			Seqcol: r.Default.Seqcol, Annotation: annotation(r.Default.Annotation), Properties: r.Default.Properties}
	}
	return s
}
