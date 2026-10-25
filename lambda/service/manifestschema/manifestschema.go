// Package manifestschema holds the app.yml JSON Schema (app-manifest v1) and
// validates manifests against it. The same file is published, from this
// repo's terraform, at its $id: https://schemas.pennsieve.io/app-manifest/v1.json.
package manifestschema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"gopkg.in/yaml.v3"
)

// URL is the schema's $id, where it's published.
const URL = "https://schemas.pennsieve.io/app-manifest/v1.json"

//go:embed app-manifest.v1.json
var V1 []byte

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(V1))
		if err != nil {
			compileErr = fmt.Errorf("reading the app.yml schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		if err := c.AddResource(URL, doc); err != nil {
			compileErr = fmt.Errorf("loading the app.yml schema: %w", err)
			return
		}
		compiled, compileErr = c.Compile(URL)
	})
	return compiled, compileErr
}

// Problem is one way a manifest doesn't follow the schema: where (a JSON
// pointer into the manifest, "" for the whole file) and why.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Validate checks app.yml text against the schema. It returns the problems
// found (none for a valid manifest), or an error when the file isn't YAML at
// all or the schema can't be loaded.
func Validate(appYml []byte) ([]Problem, error) {
	sch, err := schema()
	if err != nil {
		return nil, err
	}
	var doc any
	if err := yaml.Unmarshal(appYml, &doc); err != nil {
		return nil, fmt.Errorf("app.yml isn't valid YAML: %w", err)
	}
	// Through JSON, so the validator sees JSON's types (json.Number, string
	// keys) rather than YAML's.
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("app.yml can't be read as JSON data: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	verr := sch.Validate(inst)
	if verr == nil {
		return nil, nil
	}
	ve, ok := verr.(*jsonschema.ValidationError)
	if !ok {
		return nil, verr
	}
	return problems(ve), nil
}

// problems flattens the validator's tree of errors to its leaves, one per
// location and message.
func problems(ve *jsonschema.ValidationError) []Problem {
	printer := message.NewPrinter(language.English)
	seen := map[string]bool{}
	var out []Problem
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			p := Problem{Message: e.ErrorKind.LocalizedString(printer)}
			if len(e.InstanceLocation) > 0 {
				p.Path = "/" + strings.Join(e.InstanceLocation, "/")
			}
			if key := p.String(); !seen[key] {
				seen[key] = true
				out = append(out, p)
			}
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Summary is the problems as one line each, for logs and status messages.
func Summary(ps []Problem) string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "; ")
}
