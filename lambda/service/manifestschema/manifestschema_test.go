package manifestschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaIsValidJSONWithItsURLAsID(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(V1, &s); err != nil {
		t.Fatalf("schema isn't JSON: %v", err)
	}
	if s["$id"] != URL {
		t.Fatalf("$id = %v, want %s", s["$id"], URL)
	}
	if _, err := schema(); err != nil {
		t.Fatalf("schema doesn't compile: %v", err)
	}
}

// Real app.yml files, as their repositories have them: the two session apps
// (pennsieve-jupyter-session #9, pennsieve-r-session #4) and seurat-processor.
func TestRealManifestsAreValid(t *testing.T) {
	files, err := filepath.Glob("testdata/*.app.yml")
	if err != nil || len(files) < 3 {
		t.Fatalf("testdata: %v, %d files", err, len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ps, err := Validate(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if len(ps) > 0 {
			t.Errorf("%s: %s", f, Summary(ps))
		}
	}
}

func TestProblems(t *testing.T) {
	cases := []struct {
		name, yml, want string
	}{
		{"no application", "runtime:\n  cpu: 1024\n", "application"},
		{"a typo", "application:\n  name: a\nruntime:\n  sesionType: jupyter\n", "sesionType"},
		{"jupyter without a kernel", "application:\n  name: a\nruntime:\n  interactive: true\n  sessionType: jupyter\n", "kernel"},
		{"a session type that isn't interactive", "application:\n  name: a\nruntime:\n  sessionType: jupyter\n  kernel: python\n", "interactive"},
		{"a kernel without jupyter", "application:\n  name: a\nruntime:\n  kernel: r\n", "sessionType"},
		{"an unknown compute type", "application:\n  name: a\nruntime:\n  computeTypes: [ecs]\n", "/runtime/computeTypes/0"},
		{"a bad ORCID", "application:\n  name: a\n  maintainers:\n    - name: b\n      orcid: 1234\n", "/application/maintainers/0/orcid"},
		{"a bad media type", "application:\n  name: a\ninputs:\n  - name: x\n    mediaTypes: [parquet]\n", "/inputs/0/mediaTypes/0"},
		{"an unknown container", "application:\n  name: a\ninputs:\n  - name: x\n    container: folder\n", "/inputs/0/container"},
		{"a subfolder named with spaces", "application:\n  name: a\ninputs:\n  - name: My recordings\n    placement: subfolder\n", "/inputs/0/name"},
		{"folders of no format", "application:\n  name: a\ninputs:\n  - name: x\n    container: directory\n", "mediaTypes"},
		{"two outputs, one not folder-safe", "application:\n  name: a\noutputs:\n  - name: labels\n  - name: My Figures\n", "/outputs/1/name"},
		{"an unknown placement", "application:\n  name: a\ninputs:\n  - name: x\n    placement: nested\n", "/inputs/0/placement"},
		{"a link that isn't https", "application:\n  name: a\ninputs:\n  - name: x\n    conformsTo: [pennsieve-published-dataset, http://example.org/layout]\n", "/inputs/0/conformsTo"},
		{"a structure that isn't a registry id", "application:\n  name: a\ninputs:\n  - name: x\n    conformsTo: BIDS 1.9\n", "/inputs/0/conformsTo"},
		{"a bad EDAM format", "application:\n  name: a\noutputs:\n  - name: x\n    formats: [edf]\n", "/outputs/0/formats/0"},
		{"an unknown parameter type", "application:\n  name: a\nparameters:\n  - name: p\n    type: file\n", "/parameters/0/type"},
		{"a license, which comes from the repository", "application:\n  name: a\n  license: MIT\n", "license"},
	}
	for _, c := range cases {
		ps, err := Validate([]byte(c.yml))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(ps) == 0 || !strings.Contains(Summary(ps), c.want) {
			t.Errorf("%s: problems %q, want one mentioning %q", c.name, Summary(ps), c.want)
		}
	}
}

func TestOptionalVocabularyIsValid(t *testing.T) {
	yml := `application:
  name: Spike detector
  maintainers:
    - name: Ada
      email: ada@example.org
      orcid: https://orcid.org/0000-0002-1825-0097
    - name: Bo
      orcid: 0000-0002-1694-233X
  tags: [eeg, spikes]
runtime:
  computeTypes: [standard, gpu]
parameters:
  - name: THRESHOLD
    type: number
    defaultValue: 0.5
    min: 0
    max: 1
  - name: MODE
    type: string
    validValues: [fast, accurate]
inputs:
  - name: recording
    formats: [format_3590, http://edamontology.org/format_3549]
outputs:
  - name: spikes
    mediaTypes: [application/vnd.apache.parquet]
  - name: anything
    description: no mediaTypes or formats, so any file
`
	ps, err := Validate([]byte(yml))
	if err != nil || len(ps) > 0 {
		t.Fatalf("err %v, problems %s", err, Summary(ps))
	}
}

func TestNotYAML(t *testing.T) {
	if _, err := Validate([]byte("application: [unclosed")); err == nil {
		t.Fatal("want an error for broken YAML")
	}
}
