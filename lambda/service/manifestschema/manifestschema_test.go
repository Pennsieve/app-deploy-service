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
		{"llm that isn't a boolean", "application:\n  name: a\nruntime:\n  llm: claude\n", "/runtime/llm"},
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
  llm: true
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

// variantsYML is a minimal app with parameters, for variant cases.
func variantsYML(variants string) string {
	return "application:\n  name: a\nparameters:\n  - name: MODEL\n    validValues: [x, y]\n    defaultValue: x\nvariants:\n" + variants
}

func TestVariantsAndBuildAreValid(t *testing.T) {
	cases := map[string]string{
		"the DynaSD pilot shape": variantsYML(`  - id: cpu
    name: CPU
    default: true
    build: { args: { TORCH_FLAVOUR: cpu } }
    runtime: { computeTypes: [standard] }
  - id: gpu
    name: GPU (ONCET)
    description: CUDA PyTorch.
    build: { args: { TORCH_FLAVOUR: cuda, N: 2, DEBUG: false }, target: gpu, dockerfile: docker/Dockerfile.gpu }
    runtime: { computeTypes: [gpu], memory: 14336, cpu: 4096, timeoutSeconds: 60, llm: true }
    parameters:
      MODEL: { defaultValue: y, validValues: [y], required: true, label: Detector, description: d }
`),
		"only an id":                     variantsYML("  - id: a\n"),
		"no default: the first":          variantsYML("  - id: a\n  - id: b-2\n"),
		"default: false on several":      variantsYML("  - id: a\n    default: false\n  - id: b\n    default: false\n"),
		"a 32-character id":              variantsYML("  - id: abcdefghijklmnopqrstuvwxyz012345\n"),
		"eight variants":                 variantsYML("  - id: a\n  - id: b\n  - id: c\n  - id: d\n  - id: e\n  - id: f\n  - id: g\n  - id: h\n"),
		"a top-level build, no variants": "application:\n  name: a\nbuild:\n  args: { A_1: x }\n  target: runtime\n  dockerfile: ./Dockerfile\n",
		"a notebook app with variants":   "application:\n  name: a\nruntime:\n  interactive: true\n  sessionType: jupyter\n  kernel: python\nvariants:\n  - id: cpu\n  - id: gpu\n    runtime: { computeTypes: [gpu] }\n",
	}
	for name, yml := range cases {
		ps, err := Validate([]byte(yml))
		if err != nil || len(ps) > 0 {
			t.Errorf("%s: err %v, problems %s", name, err, Summary(ps))
		}
	}
}

func TestVariantProblems(t *testing.T) {
	cases := []struct {
		name, yml, want string
	}{
		{"no variants in the list", variantsYML("  []\n"), "/variants"},
		{"nine variants", variantsYML("  - id: a\n  - id: b\n  - id: c\n  - id: d\n  - id: e\n  - id: f\n  - id: g\n  - id: h\n  - id: i\n"), "/variants"},
		{"no id", variantsYML("  - name: CPU\n"), "id"},
		{"an uppercase id", variantsYML("  - id: GPU\n"), "/variants/0/id"},
		{"a leading dash", variantsYML("  - id: -gpu\n"), "/variants/0/id"},
		{"a trailing dash", variantsYML("  - id: gpu-\n"), "/variants/0/id"},
		{"a double dash", variantsYML("  - id: gpu--a\n"), "/variants/0/id"},
		{"a 33-character id", variantsYML("  - id: abcdefghijklmnopqrstuvwxyz0123456\n"), "/variants/0/id"},
		{"the reserved id", variantsYML("  - id: default\n"), "/variants/0/id"},
		{"two defaults", variantsYML("  - id: a\n    default: true\n  - id: b\n    default: true\n"), "/variants"},
		{"inputs on a variant", variantsYML("  - id: a\n    inputs: [{name: x}]\n"), "inputs"},
		{"outputs on a variant", variantsYML("  - id: a\n    outputs: [{name: x}]\n"), "outputs"},
		{"tags on a variant", variantsYML("  - id: a\n    tags: [gpu]\n"), "tags"},
		{"an application on a variant", variantsYML("  - id: a\n    application: {name: b}\n"), "application"},
		{"a parameter list on a variant", variantsYML("  - id: a\n    parameters: [{name: MODEL}]\n"), "/variants/0/parameters"},
		{"a parameter's type changed", variantsYML("  - id: a\n    parameters: { MODEL: { type: number } }\n"), "type"},
		{"interactive on a variant", variantsYML("  - id: a\n    runtime: { interactive: true }\n"), "interactive"},
		{"a session type on a variant", variantsYML("  - id: a\n    runtime: { sessionType: jupyter }\n"), "sessionType"},
		{"a kernel on a variant", variantsYML("  - id: a\n    runtime: { kernel: r }\n"), "kernel"},
		{"an unknown compute type on a variant", variantsYML("  - id: a\n    runtime: { computeTypes: [tpu] }\n"), "/variants/0/runtime/computeTypes/0"},
		{"a zero memory on a variant", variantsYML("  - id: a\n    runtime: { memory: 0 }\n"), "/variants/0/runtime/memory"},
		{"a build arg that isn't a scalar", variantsYML("  - id: a\n    build: { args: { A: [1, 2] } }\n"), "/variants/0/build/args/A"},
		{"a build arg that isn't a name", variantsYML("  - id: a\n    build: { args: { 1A: x } }\n"), "'1A' does not match"},
		{"an unknown build field", "application:\n  name: a\nbuild:\n  context: sub\n", "context"},
		{"an absolute dockerfile", "application:\n  name: a\nbuild:\n  dockerfile: /etc/Dockerfile\n", "/build/dockerfile"},
		{"a dockerfile outside the repository", variantsYML("  - id: a\n    build: { dockerfile: docker/../../Dockerfile }\n"), "/variants/0/build/dockerfile"},
		{"a dockerfile of ..", "application:\n  name: a\nbuild:\n  dockerfile: ..\n", "/build/dockerfile"},
		{"variants that aren't a list", "application:\n  name: a\nvariants:\n  cpu: {}\n", "/variants"},
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
