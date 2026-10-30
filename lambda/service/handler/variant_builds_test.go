package handler

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sequentialIds() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("dep-%d", n)
	}
}

// Every variant is built: the default by the release's deployment (its
// status stays the version's), the others by their own, "registering".
func TestPlanVariantBuilds_EveryVariant(t *testing.T) {
	r := resolveVariants(readConfig(t, dynasdVariantsFile))
	vv := r.versionVariants()

	planned := planVariantBuilds(&vv, r, "release-dep", true, sequentialIds())

	assert.Equal(t, []variantDeployment{{"cpu", "release-dep"}, {"gpu", "dep-1"}}, planned)
	cpu, gpu := vv.Variants["cpu"], vv.Variants["gpu"]
	assert.Equal(t, "release-dep", cpu.DeploymentId)
	assert.Empty(t, cpu.Status, "the default variant's status is the version's until its build ends")
	assert.Zero(t, cpu.BuildStorageGiB)
	assert.Equal(t, "dep-1", gpu.DeploymentId)
	assert.Equal(t, "registering", gpu.Status)
	assert.Equal(t, gpuBuildStorageGiB, gpu.BuildStorageGiB, "the gpu variant's build gets the GPU disk")
	assert.Equal(t, "cpu", r.defaultVariantBuilt(planned))
}

// APP_VARIANTS_ENABLED=false: only the default variant is built.
func TestPlanVariantBuilds_OnlyDefault(t *testing.T) {
	r := resolveVariants(readConfig(t, dynasdVariantsFile))
	vv := r.versionVariants()

	planned := planVariantBuilds(&vv, r, "release-dep", false, sequentialIds())

	assert.Equal(t, []variantDeployment{{"cpu", "release-dep"}}, planned)
	assert.Empty(t, vv.Variants["gpu"].DeploymentId)
	assert.Empty(t, vv.Variants["gpu"].Status)
}

// The default variant's deployment comes first even when it isn't listed first.
func TestPlanVariantBuilds_DefaultNotFirst(t *testing.T) {
	c, err := parseAppConfig([]byte(`
runtime: {computeTypes: [standard]}
variants:
  - id: gpu
    runtime: {computeTypes: [gpu]}
  - id: cpu
    default: true
`))
	require.NoError(t, err)
	r := resolveVariants(c)
	vv := r.versionVariants()

	planned := planVariantBuilds(&vv, r, "release-dep", true, sequentialIds())
	assert.Equal(t, []variantDeployment{{"cpu", "release-dep"}, {"gpu", "dep-1"}}, planned)
}

// No variants: nothing to plan, with or without a top-level build.
func TestPlanVariantBuilds_NoVariants(t *testing.T) {
	for _, yml := range []string{"runtime: {cpu: 1024}\n", "build: {args: {A: b}}\n"} {
		c, err := parseAppConfig([]byte(yml))
		require.NoError(t, err)
		r := resolveVariants(c)
		vv := r.versionVariants()
		assert.Nil(t, planVariantBuilds(&vv, r, "release-dep", true, sequentialIds()), yml)
		assert.Empty(t, r.defaultVariantBuilt(nil))
	}
}

func TestBuiltDockerfiles(t *testing.T) {
	parse := func(yml string) resolvedApp {
		c, err := parseAppConfig([]byte(yml))
		require.NoError(t, err)
		return resolveVariants(c)
	}

	assert.Equal(t, []string{"Dockerfile"}, builtDockerfiles(parse("runtime: {cpu: 1024}\n"), true))
	assert.Equal(t, []string{"docker/Dockerfile.cpu"}, builtDockerfiles(parse("build: {dockerfile: ./docker/Dockerfile.cpu}\n"), true))

	variants := parse(`
build: {dockerfile: Dockerfile}
variants:
  - id: cpu
  - id: gpu
    build: {dockerfile: docker/Dockerfile.gpu}
  - id: gpu-large
    build: {dockerfile: docker/Dockerfile.gpu, target: large}
`)
	assert.Equal(t, []string{"Dockerfile", "docker/Dockerfile.gpu"}, builtDockerfiles(variants, true), "each distinct Dockerfile once")
	assert.Equal(t, []string{"Dockerfile"}, builtDockerfiles(variants, false), "only the default's when only it is built")
}

func TestVariantBuildsEnabled(t *testing.T) {
	for value, want := range map[string]bool{"": true, "true": true, "false": false, " FALSE ": false, "0": true} {
		t.Setenv(appVariantsEnabledEnv, value)
		assert.Equal(t, want, variantBuildsEnabled(), "%q", value)
	}
}
