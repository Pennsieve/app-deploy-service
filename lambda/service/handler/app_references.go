package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// appReference is one entry of app.yml's references: shared, published data
// the app reads from the folder path in its Name variable, described by public
// identifiers so the app isn't tied to Pennsieve. On Pennsieve it resolves to
// an official reference in environments-service's catalog.
type appReference struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	Kind        string               `yaml:"kind"`
	AssetClass  string               `yaml:"assetClass"`
	ToolVersion string               `yaml:"toolVersion"`
	Choice      string               `yaml:"choice"`
	TaxonomyID  int                  `yaml:"taxonomyId"`
	Assembly    string               `yaml:"assembly"`
	Seqcol      string               `yaml:"seqcol"`
	Annotation  *referenceAnnotation `yaml:"annotation"`
	Organisms   []int                `yaml:"organisms"`
	Default     *referenceIdentity   `yaml:"default"`
	Source      string               `yaml:"source"`
	SHA256      string               `yaml:"sha256"`
	License     string               `yaml:"license"`
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
}

// Variables the platform sets, which a reference can't take.
var reservedReferenceNames = map[string]bool{"INPUT_DIR": true, "OUTPUT_DIR": true, "REFERENCES_DIR": true}

// referenceProblems are the checks on app.yml's references that JSON Schema
// can't express: names are unique and aren't a parameter's or the platform's,
// a choice: user default is one of its organisms, and the app (and each
// variant) runs somewhere references are delivered, not only on Lambda.
func referenceProblems(c appConfig) []string {
	if len(c.References) == 0 {
		return nil
	}
	var out []string
	params := map[string]bool{}
	for _, p := range c.Parameters {
		params[p.Name] = true
	}
	seen := map[string]int{}
	for i, r := range c.References {
		path := fmt.Sprintf("/references/%d", i)
		if first, dup := seen[r.Name]; dup && r.Name != "" {
			out = append(out, fmt.Sprintf("%s/name: %q is already the name of reference %d; reference names must be unique", path, r.Name, first))
		} else {
			seen[r.Name] = i
		}
		if params[r.Name] {
			out = append(out, fmt.Sprintf("%s/name: %q is also a parameter's name; a reference needs its own variable", path, r.Name))
		}
		if reservedReferenceNames[r.Name] {
			out = append(out, fmt.Sprintf("%s/name: %q is set by the platform", path, r.Name))
		}
		if r.Choice == "user" && r.Default != nil && r.Default.TaxonomyID != 0 && len(r.Organisms) > 0 && !containsInt(r.Organisms, r.Default.TaxonomyID) {
			out = append(out, fmt.Sprintf("%s/default/taxonomyId: %d is not one of the reference's organisms %v", path, r.Default.TaxonomyID, r.Organisms))
		}
		if r.Choice == "user" && r.TaxonomyID != 0 {
			out = append(out, fmt.Sprintf("%s/taxonomyId: a choice: user reference lists the organisms a run may pick in organisms", path))
		}
	}
	// References are delivered to ECS steps (standard, gpu), not Lambda ones.
	if lambdaOnly(defaultComputeTypes(c.Runtime.ComputeTypes)) {
		out = append(out, "/runtime/computeTypes: an app with references can't run only on lambda; references are delivered to standard and gpu steps")
	}
	for i, v := range c.Variants {
		if v.Runtime.ComputeTypes != nil && lambdaOnly(v.Runtime.ComputeTypes) {
			out = append(out, fmt.Sprintf("/variants/%d/runtime/computeTypes: an app with references can't run only on lambda; references are delivered to standard and gpu steps", i))
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
		Genomes []json.RawMessage `json:"genomes"`
		Message string            `json:"message"`
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
	return len(body.Genomes), nil
}

// catalogRefusal is the catalog saying the query itself is wrong (a bad
// toolVersion): a problem with the app.yml, not with the catalog.
type catalogRefusal string

func (e catalogRefusal) Error() string { return string(e) }

// referenceQuery is the catalog query for an entry, narrowed to one identity
// (a pinned reference, a default, or one organism).
func referenceQuery(r appReference, id referenceIdentity) url.Values {
	q := url.Values{"kind": {r.Kind}, "assetClass": {r.AssetClass}}
	if r.ToolVersion != "" {
		q.Set("toolVersion", r.ToolVersion)
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

// referenceCatalogProblems checks that each reference resolves in the
// catalog: a pinned one to a reference, a choice: user one to at least one
// per organism it offers, and its default to one. A catalog that can't be
// reached is logged and not held against the app.
func referenceCatalogProblems(ctx context.Context, cat referenceCatalog, c appConfig) []string {
	if cat == nil || len(c.References) == 0 {
		return nil
	}
	var out []string
	for i, r := range c.References {
		path := fmt.Sprintf("/references/%d", i)
		type check struct {
			path string
			id   referenceIdentity
			what string
		}
		var checks []check
		switch r.Choice {
		case "app":
			checks = append(checks, check{path, referenceIdentity{r.TaxonomyID, r.Assembly, r.Seqcol, r.Annotation}, "the pinned reference"})
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
func describeQuery(r appReference, id referenceIdentity) string {
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
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, ", ")
}

// storedReferences is app.yml's references as the version records them.
func storedReferences(refs []appReference) []store_dynamodb.AppReference {
	if len(refs) == 0 {
		return nil
	}
	annotation := func(a *referenceAnnotation) *store_dynamodb.ReferenceAnnotation {
		if a == nil {
			return nil
		}
		return &store_dynamodb.ReferenceAnnotation{Provider: a.Provider, Version: a.Version}
	}
	out := make([]store_dynamodb.AppReference, 0, len(refs))
	for _, r := range refs {
		s := store_dynamodb.AppReference{
			Name: r.Name, Description: r.Description, Kind: r.Kind, AssetClass: r.AssetClass, ToolVersion: r.ToolVersion,
			Choice: r.Choice, TaxonomyID: r.TaxonomyID, Assembly: r.Assembly, Seqcol: r.Seqcol, Annotation: annotation(r.Annotation),
			Organisms: r.Organisms, Source: r.Source, SHA256: r.SHA256, License: r.License,
		}
		if r.Default != nil {
			s.Default = &store_dynamodb.ReferenceIdentity{TaxonomyID: r.Default.TaxonomyID, Assembly: r.Default.Assembly,
				Seqcol: r.Default.Seqcol, Annotation: annotation(r.Default.Annotation)}
		}
		out = append(out, s)
	}
	return out
}
