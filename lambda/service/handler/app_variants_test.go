package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The DynaSD pilot (docs/app-variants.md in workflow-service, section 2):
// cpu (default) and gpu (CUDA, [gpu], 14336 MiB, MODEL oncet).
const dynasdVariantsFile = "../manifestschema/testdata/processor-seizure-dynasd-variants.app.yml"

func readConfig(t *testing.T, path string) appConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	c, err := parseAppConfig(data)
	require.NoError(t, err)
	return c
}

func param(m *store_dynamodb.AppManifest, name string) store_dynamodb.AppParameter {
	for _, p := range m.Parameters {
		if p.Name == name {
			return p
		}
	}
	return store_dynamodb.AppParameter{}
}

func TestResolveVariants_DynaSDPilot(t *testing.T) {
	c := readConfig(t, dynasdVariantsFile)
	r := resolveVariants(c)

	require.Len(t, r.Variants, 2)
	assert.Equal(t, "cpu", r.DefaultVariant)

	cpu, gpu := r.Variants[0], r.Variants[1]
	assert.Equal(t, "CPU", cpu.Name)
	assert.Equal(t, "GPU (ONCET)", gpu.Name)
	assert.Contains(t, gpu.Description, "NVIDIA T4")

	// runtime: inherited, then replaced field by field
	assert.Equal(t, 4096, cpu.Config.Runtime.CPU)
	assert.Equal(t, 8192, cpu.Config.Runtime.Memory)
	assert.Equal(t, []string{"standard"}, cpu.Config.Runtime.ComputeTypes)
	assert.Equal(t, 14400, cpu.Config.Runtime.TimeoutSeconds)
	assert.Equal(t, 4096, gpu.Config.Runtime.CPU)
	assert.Equal(t, 14336, gpu.Config.Runtime.Memory)
	assert.Equal(t, []string{"gpu"}, gpu.Config.Runtime.ComputeTypes)
	assert.Equal(t, 14400, gpu.Config.Runtime.TimeoutSeconds)

	// parameters: MODEL overridden on gpu only; the rest identical
	cpuM, gpuM := cpu.manifest(), gpu.manifest()
	assert.Equal(t, "lindda", param(cpuM, "MODEL").DefaultValue)
	assert.Equal(t, "oncet", param(gpuM, "MODEL").DefaultValue)
	assert.Equal(t, param(cpuM, "MODEL").ValidValues, param(gpuM, "MODEL").ValidValues)
	assert.Equal(t, "auto", param(gpuM, "DEVICE").DefaultValue)
	assert.Equal(t, len(cpuM.Parameters), len(gpuM.Parameters))

	// application, ports and tags are the top level's
	assert.Equal(t, cpuM.Name, gpuM.Name)
	assert.Equal(t, "Seizure Onset (DynaSD)", gpuM.Name)
	assert.Equal(t, cpuM.Tags, gpuM.Tags)
	assert.Equal(t, cpuM.Inputs, gpuM.Inputs)
	assert.Equal(t, cpuM.Outputs, gpuM.Outputs)

	// the manifest names its variant
	assert.Equal(t, &store_dynamodb.AppManifestVariant{ID: "gpu", Name: "GPU (ONCET)", Description: gpu.Description}, gpuM.Variant)

	// build: variant args over the top-level args
	assert.Equal(t, map[string]string{"DYNASD_COMMIT": "48b2b959605a37fb03a4d99ae6fcc1459c04cc37", "TORCH_FLAVOUR": "cpu"}, cpu.Build.Args)
	assert.Equal(t, map[string]string{"DYNASD_COMMIT": "48b2b959605a37fb03a4d99ae6fcc1459c04cc37", "TORCH_FLAVOUR": "cuda"}, gpu.Build.Args)

	// the version's manifest and the app's params are the default's
	vm := r.versionManifest()
	assert.Equal(t, cpuM, vm)
	assert.Equal(t, "cpu", vm.Variant.ID)
	assert.Equal(t, "lindda", param(vm, "MODEL").DefaultValue)
	assert.Equal(t, cpuM.Parameters, r.parameters())

	// and the pilot has no problems
	data, err := os.ReadFile(dynasdVariantsFile)
	require.NoError(t, err)
	assert.Empty(t, manifestProblems(data))
}

func TestResolveVariants_DefaultSelection(t *testing.T) {
	base := "application: {name: a}\nvariants:\n"
	cases := []struct{ name, variants, want string }{
		{"the first without default: true", "  - id: a\n  - id: b\n", "a"},
		{"default: true wins over order", "  - id: a\n  - id: b\n    default: true\n", "b"},
		{"default: false is not a default", "  - id: a\n    default: false\n  - id: b\n", "a"},
		{"two defaults (a problem): the first", "  - id: a\n  - id: b\n    default: true\n  - id: c\n    default: true\n", "b"},
		{"an invalid default is skipped", "  - id: Bad\n    default: true\n  - id: b\n", "b"},
	}
	for _, c := range cases {
		cfg, err := parseAppConfig([]byte(base + c.variants))
		require.NoError(t, err, c.name)
		r := resolveVariants(cfg)
		assert.Equal(t, c.want, r.DefaultVariant, c.name)
		assert.Equal(t, c.want, r.versionManifest().Variant.ID, c.name)
	}
}

func TestResolveVariants_Overrides(t *testing.T) {
	cfg, err := parseAppConfig([]byte(`application: {name: a, tags: [eeg]}
runtime:
  cpu: 1024
  memory: 2048
  computeTypes: [standard, gpu]
  timeoutSeconds: 60
  llm: true
  interactive: true
  sessionType: jupyter
  kernel: python
build:
  args: {A: top, B: top}
  target: base
  dockerfile: Dockerfile
parameters:
  - name: P
    label: Pee
    description: top
    defaultValue: 1
    validValues: [1, 2]
  - name: Q
    defaultValue: q
variants:
  - id: small
  - id: big
    name: Big
    build: {args: {B: big, C: 3}, target: gpu, dockerfile: docker/Dockerfile.gpu}
    runtime: {cpu: 4096, computeTypes: [gpu], llm: false}
    parameters:
      P: {defaultValue: 2, validValues: [2], required: true, description: big}
      NOPE: {defaultValue: x}
`))
	require.NoError(t, err)
	r := resolveVariants(cfg)
	require.Len(t, r.Variants, 2)
	small, big := r.Variants[0], r.Variants[1]

	// an empty variant is the top level, named by its id
	assert.Equal(t, "small", small.Name)
	topM := appManifest(r.Default)
	smallM := small.manifest()
	smallM.Variant = nil
	assert.Equal(t, topM, smallM)
	assert.Equal(t, appBuild{Args: map[string]string{"A": "top", "B": "top"}, Target: "base", Dockerfile: "Dockerfile"}, small.Build)

	// runtime fields replaced, not merged; interactive/session settings kept
	rt := big.Config.Runtime
	assert.Equal(t, 4096, rt.CPU)
	assert.Equal(t, 2048, rt.Memory)
	assert.Equal(t, []string{"gpu"}, rt.ComputeTypes)
	assert.Equal(t, 60, rt.TimeoutSeconds)
	assert.False(t, rt.LLM)
	assert.True(t, rt.Interactive)
	assert.Equal(t, "jupyter", rt.SessionType)
	assert.Equal(t, "python", rt.Kernel)

	// parameter overrides; unknown names ignored; no parameter added
	bigM := big.manifest()
	require.Len(t, bigM.Parameters, 2)
	assert.Equal(t, store_dynamodb.AppParameter{Name: "P", Required: true, Description: "big", DefaultValue: "2", ValidValues: []string{"2"}}, bigM.Parameters[0])
	assert.Equal(t, "q", bigM.Parameters[1].DefaultValue)
	assert.Equal(t, []string{"eeg"}, bigM.Tags)

	// build merged
	assert.Equal(t, appBuild{Args: map[string]string{"A": "top", "B": "big", "C": "3"}, Target: "gpu", Dockerfile: "docker/Dockerfile.gpu"}, big.Build)

	// the top level isn't changed by resolving
	assert.Equal(t, "1", cfg.Parameters[0].DefaultValue)
	assert.Equal(t, []string{"1", "2"}, cfg.Parameters[0].ValidValues)
	assert.Equal(t, 1024, cfg.Runtime.CPU)
}

func TestResolveVariants_SkipsUnsafeIdsAndCaps(t *testing.T) {
	var b strings.Builder
	b.WriteString("application: {name: a}\nvariants:\n")
	for _, id := range []string{"a", "a", "default", "x--y", "Up", "b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString("  - id: " + id + "\n")
	}
	cfg, err := parseAppConfig([]byte(b.String()))
	require.NoError(t, err)
	r := resolveVariants(cfg)
	assert.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, r.versionVariants().VariantOrder)
}

func TestVersionVariants(t *testing.T) {
	r := resolveVariants(readConfig(t, dynasdVariantsFile))
	vv := r.versionVariants()
	assert.Equal(t, "cpu", vv.DefaultVariant)
	assert.Equal(t, []string{"cpu", "gpu"}, vv.VariantOrder)
	require.Len(t, vv.Variants, 2)
	assert.Nil(t, vv.Build, "with variants, each variant carries its build")
	gpu := vv.Variants["gpu"]
	assert.Equal(t, "GPU (ONCET)", gpu.Name)
	assert.Equal(t, "oncet", param(gpu.Manifest, "MODEL").DefaultValue)
	assert.Equal(t, &store_dynamodb.AppBuild{Args: map[string]string{"DYNASD_COMMIT": "48b2b959605a37fb03a4d99ae6fcc1459c04cc37", "TORCH_FLAVOUR": "cuda"}}, gpu.Build)
	assert.Equal(t, r.versionManifest(), vv.Variants["cpu"].Manifest)

	// a top-level build without variants is kept on the version
	cfg, err := parseAppConfig([]byte("application: {name: a}\nbuild:\n  args: {X: 1}\n  target: t\n"))
	require.NoError(t, err)
	vv = resolveVariants(cfg).versionVariants()
	assert.Equal(t, store_dynamodb.VersionVariants{Build: &store_dynamodb.AppBuild{Args: map[string]string{"X": "1"}, Target: "t"}}, vv)
	assert.False(t, vv.Empty())
	assert.Nil(t, resolveVariants(cfg).versionManifest().Variant)
}

// An app.yml without variants is stored exactly as before variants: the
// golden file is what the code before them stored (manifest and params) for
// each real app.yml in manifestschema/testdata and gpuAppYAML. Nothing else
// is recorded on the version.
func TestNoVariants_StoredManifestUnchanged(t *testing.T) {
	raw, err := os.ReadFile("testdata/stored-manifests.golden.json")
	require.NoError(t, err)
	var golden map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &golden))
	require.GreaterOrEqual(t, len(golden), 5)
	for name, want := range golden {
		var data []byte
		if name == "gpuAppYAML" {
			data = []byte(gpuAppYAML)
		} else {
			data, err = os.ReadFile(filepath.Join("../manifestschema/testdata", name))
			require.NoError(t, err)
		}
		c, err := parseAppConfig(data)
		require.NoError(t, err)
		r := resolveVariants(c)
		av, err := attributevalue.Marshal(r.versionManifest())
		require.NoError(t, err)
		pav, err := attributevalue.Marshal(r.parameters())
		require.NoError(t, err)
		got, err := json.MarshalIndent(map[string]any{"manifest": av, "params": pav}, " ", " ")
		require.NoError(t, err)
		var wantV, gotV any
		require.NoError(t, json.Unmarshal(want, &wantV))
		require.NoError(t, json.Unmarshal(got, &gotV))
		assert.Equal(t, wantV, gotV, name)
		assert.True(t, r.versionVariants().Empty(), "%s records no variants", name)
	}
}

func TestVariantProblems(t *testing.T) {
	base := `application: {name: a}
parameters:
  - name: MODEL
    defaultValue: x
    validValues: [x, y]
  - name: N
    type: number
    defaultValue: 5
    min: 1
    max: 10
  - name: FREE
    defaultValue: f
variants:
`
	cases := []struct {
		name, variants string
		want           []string // empty: no problems
	}{
		{"valid overrides", "  - id: a\n    parameters:\n      MODEL: {defaultValue: y}\n      N: {defaultValue: 10}\n      FREE: {defaultValue: g, validValues: [g, h]}\n", nil},
		{"a number written differently", "  - id: a\n    parameters:\n      N: {defaultValue: 2.0, validValues: [1, 2]}\n", nil},
		{"duplicate ids", "  - id: a\n  - id: b\n  - id: a\n", []string{`/variants/2/id: "a" is already the id of variant 0`}},
		{"an unknown parameter", "  - id: a\n    parameters:\n      MODLE: {defaultValue: y}\n", []string{`/variants/0/parameters/MODLE: there is no top-level parameter "MODLE"`}},
		{"a default outside the inherited validValues", "  - id: a\n    parameters:\n      MODEL: {defaultValue: z}\n", []string{`/variants/0/parameters/MODEL/defaultValue: "z" is not one of`}},
		{"a default outside the overridden validValues", "  - id: a\n    parameters:\n      MODEL: {defaultValue: x, validValues: [y]}\n", []string{`/variants/0/parameters/MODEL/defaultValue: "x" is not one of`}},
		{"validValues that drop the inherited default", "  - id: a\n    parameters:\n      MODEL: {validValues: [y]}\n", []string{`/variants/0/parameters/MODEL/defaultValue: "x" is not one of`}},
		{"below min", "  - id: a\n    parameters:\n      N: {defaultValue: 0}\n", []string{"/variants/0/parameters/N/defaultValue: 0 is less than the parameter's min 1"}},
		{"above max", "  - id: a\n    parameters:\n      N: {defaultValue: 11}\n", []string{"/variants/0/parameters/N/defaultValue: 11 is more than the parameter's max 10"}},
		{"not a number", "  - id: a\n    parameters:\n      N: {defaultValue: many}\n", []string{`/variants/0/parameters/N/defaultValue: "many" is not a number`}},
		{"a label only", "  - id: a\n    parameters:\n      N: {label: Count}\n", nil},
	}
	for _, c := range cases {
		cfg, err := parseAppConfig([]byte(base + c.variants))
		require.NoError(t, err, c.name)
		got := variantProblems(cfg)
		if len(c.want) == 0 {
			assert.Empty(t, got, c.name)
			continue
		}
		require.Len(t, got, len(c.want), "%s: %v", c.name, got)
		for i, w := range c.want {
			assert.Contains(t, got[i], w, c.name)
		}
	}

	// manifestProblems reports them with the schema's, under the same mode
	got := manifestProblems([]byte(base + "  - id: a\n    parameters:\n      MODLE: {defaultValue: y}\n"))
	assert.Equal(t, []string{`/variants/0/parameters/MODLE: there is no top-level parameter "MODLE"; a variant can only override existing parameters`}, got)
}

func TestVariantTagProblem(t *testing.T) {
	withVariants := readConfig(t, dynasdVariantsFile)
	without := readConfig(t, "../manifestschema/testdata/processor-seizure-dynasd.app.yml")
	assert.Empty(t, variantTagProblem(withVariants, "v0.3.0"))
	assert.Empty(t, variantTagProblem(withVariants, "v0.3.0-rc-1"))
	assert.Contains(t, variantTagProblem(withVariants, "v0.3.0--gpu"), `contains "--"`)
	assert.Empty(t, variantTagProblem(without, "v0.3.0--gpu"), "only apps with variants")
}

func TestBackfillManifests_Variants(t *testing.T) {
	apps := &fakeApps{
		apps:   []store_dynamodb.AppStoreApplication{{Uuid: "d", SourceUrl: "https://github.com/o/dynasd"}},
		params: map[string][]store_dynamodb.AppParameter{},
	}
	versions := &fakeVersions{
		versions:  []store_dynamodb.AppStoreVersion{{Uuid: "d1", ApplicationId: "d", Version: "v0.3.0", CreatedAt: "1"}},
		manifests: map[string]*store_dynamodb.AppManifest{},
	}
	pilot := storedManifestOf(readConfig(t, dynasdVariantsFile))
	read := func(context.Context, string, string) (storedManifest, bool) { return pilot, true }

	res, err := BackfillManifests(context.Background(), apps, versions, read, false, func(string, ...any) {})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Stored)
	assert.Equal(t, "cpu", versions.manifests["d1"].Variant.ID, "the manifest is the default variant's")
	assert.Equal(t, []string{"cpu", "gpu"}, versions.variants["d1"].VariantOrder)
	assert.Equal(t, "lindda", apps.params["d"][0].DefaultValue, "the app's params are the default variant's")
}
