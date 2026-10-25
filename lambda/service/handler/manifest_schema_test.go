package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/manifestschema"
)

// Every field the parser reads is one the schema declares, at the same place:
// the two can't drift apart.
func TestParserFieldsAreInTheSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(manifestschema.V1, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	var props func(node map[string]any) map[string]any
	props = func(node map[string]any) map[string]any {
		if ref, ok := node["$ref"].(string); ok {
			node = defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		}
		if items, ok := node["items"].(map[string]any); ok {
			return props(items)
		}
		p, _ := node["properties"].(map[string]any)
		return p
	}
	var walk func(rt reflect.Type, schemaProps map[string]any, path string)
	walk = func(rt reflect.Type, schemaProps map[string]any, path string) {
		for rt.Kind() == reflect.Slice || rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			sub, ok := schemaProps[name].(map[string]any)
			if !ok {
				t.Errorf("the parser reads %s%s, which the schema doesn't declare", path, name)
				continue
			}
			walk(f.Type, props(sub), path+name+".")
		}
	}
	walk(reflect.TypeOf(appConfig{}), schema["properties"].(map[string]any), "")
}

func TestAppManifestKeepsMaintainersAndFormats(t *testing.T) {
	c, err := parseAppConfig([]byte(`application:
  name: Spikes
  maintainers:
    - name: Ada
      email: ada@example.org
      orcid: https://orcid.org/0000-0002-1825-0097
    - name: ""
inputs:
  - name: recording
    formats: [http://edamontology.org/format_3590, format_3549]
outputs:
  - name: anything
`))
	if err != nil {
		t.Fatal(err)
	}
	m := appManifest(c)
	if len(m.Maintainers) != 1 || m.Maintainers[0].Orcid != "0000-0002-1825-0097" || m.Maintainers[0].Email != "ada@example.org" {
		t.Errorf("maintainers = %+v", m.Maintainers)
	}
	if got := m.Inputs[0].Formats; !reflect.DeepEqual(got, []string{"format_3590", "format_3549"}) {
		t.Errorf("formats = %v", got)
	}
	if m.Outputs[0].Formats != nil || m.Outputs[0].MediaTypes != nil {
		t.Errorf("an output without types takes any file: %+v", m.Outputs[0])
	}
}

func TestManifestProblemsAndMode(t *testing.T) {
	if ps := manifestProblems([]byte("application:\n  name: a\n")); len(ps) != 0 {
		t.Errorf("valid manifest: %v", ps)
	}
	ps := manifestProblems([]byte("application:\n  name: a\nruntime:\n  sesionType: jupyter\n"))
	if len(ps) == 0 || !strings.Contains(strings.Join(ps, ";"), "sesionType") {
		t.Errorf("typo: %v", ps)
	}
	if ps := manifestProblems([]byte("application: [x")); len(ps) != 1 {
		t.Errorf("not YAML: %v", ps)
	}
	for env, want := range map[string]string{"": "warn", "warn": "warn", "ENFORCE": "enforce", "bogus": "warn"} {
		t.Setenv("APP_MANIFEST_VALIDATION", env)
		if got := manifestValidationMode(); got != want {
			t.Errorf("APP_MANIFEST_VALIDATION=%q: %s, want %s", env, got, want)
		}
	}
}

func TestRepoLicense(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/mit/license":
			if r.URL.Query().Get("ref") != "v1.0.0" || r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"license":{"spdx_id":"MIT"}}`))
		case "/repos/o/other/license":
			_, _ = w.Write([]byte(`{"license":{"spdx_id":"NOASSERTION"}}`))
		case "/repos/o/broken/license":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	saved := githubAPIURL
	githubAPIURL = srv.URL
	t.Cleanup(func() { githubAPIURL = saved })

	ctx := context.Background()
	if id, err := repoLicense(ctx, "https://github.com/o/mit.git", "v1.0.0", "tok"); err != nil || id != "MIT" {
		t.Errorf("mit: %q, %v", id, err)
	}
	if id, err := repoLicense(ctx, "https://github.com/o/other", "", ""); err != nil || id != "" {
		t.Errorf("NOASSERTION: %q, %v", id, err)
	}
	if id, err := repoLicense(ctx, "https://github.com/o/none/", "", ""); err != nil || id != "" {
		t.Errorf("no LICENSE: %q, %v", id, err)
	}
	if _, err := repoLicense(ctx, "https://github.com/o/broken", "", ""); err == nil {
		t.Error("a failed lookup is an error, for the log")
	}
	if _, err := repoLicense(ctx, "https://gitlab.com/o/x", "", ""); err == nil {
		t.Error("not a GitHub URL")
	}
}
