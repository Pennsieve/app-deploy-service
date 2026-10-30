package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/imagebuild"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEcrRepo   = "123456789012.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1"
	testSourceUrl = "https://github.com/pennsieve-apps/processor-seizure-dynasd"
)

type fakeVersionStore struct {
	builds       store_dynamodb.VersionBuilds
	getBuildsErr error
	gotBuilds    int
	statuses     []string
	destinations []string
	variants     map[string][]store_dynamodb.VariantUpdate
}

func (f *fakeVersionStore) UpdateStatus(_ context.Context, s string, _ string) error {
	f.statuses = append(f.statuses, s)
	return nil
}
func (f *fakeVersionStore) UpdateDestinationUrl(_ context.Context, _ string, d string, s string) error {
	f.destinations = append(f.destinations, d)
	f.statuses = append(f.statuses, s)
	return nil
}
func (f *fakeVersionStore) GetBuilds(context.Context, string) (store_dynamodb.VersionBuilds, error) {
	f.gotBuilds++
	return f.builds, f.getBuildsErr
}
func (f *fakeVersionStore) UpdateVariant(_ context.Context, _ string, variant string, u store_dynamodb.VariantUpdate) error {
	if f.variants == nil {
		f.variants = map[string][]store_dynamodb.VariantUpdate{}
	}
	f.variants[variant] = append(f.variants[variant], u)
	return nil
}

type fakeBuilder struct {
	requests []imagebuild.Request
	fail     map[string]error // by variant
}

func (fakeBuilder) Name() string { return "fake" }
func (b *fakeBuilder) Start(_ context.Context, r imagebuild.Request, _ string) error {
	b.requests = append(b.requests, r)
	return b.fail[r.Variant]
}

type fakeDeployments struct{ errored []string }

func (f *fakeDeployments) SetErroredFlag(_ context.Context, _ string, deploymentId string) error {
	f.errored = append(f.errored, deploymentId)
	return nil
}

func addToAppstore(t *testing.T, store *fakeVersionStore, builder *fakeBuilder, deployments *fakeDeployments, storage int32, readBuilds bool) error {
	t.Helper()
	t.Setenv("APPSTORE_PRIVATE_ECR_URL", testEcrRepo)
	t.Setenv("APPLICATIONS_TABLE", "dev-appstore-versions")
	m := status.NewAppStoreManager(store, "version-1")
	return AddToAppstore(context.Background(), "version-1", "release-dep", testSourceUrl, "v0.3.0", "", storage, builder, m, store, deployments, readBuilds)
}

func releaseDestination(t *testing.T, store *fakeVersionStore) string {
	t.Helper()
	require.NotEmpty(t, store.destinations)
	return store.destinations[0]
}

// Regression: an app without variants and without build settings is built
// exactly as before variants: one build, the same request, the version's
// own tag, and the version record isn't read.
func TestAddToAppstore_NoVariantsUnchanged(t *testing.T) {
	store, builder, deps := &fakeVersionStore{}, &fakeBuilder{}, &fakeDeployments{}
	require.NoError(t, addToAppstore(t, store, builder, deps, 100, false))

	assert.Zero(t, store.gotBuilds, "nothing to read")
	dest := releaseDestination(t, store)
	assert.Regexp(t, `^`+testEcrRepo+`:\d+-v0\.3\.0$`, dest)
	assert.Equal(t, []string{"deploying", "deploying"}, store.statuses)
	assert.Empty(t, store.variants)
	require.Len(t, builder.requests, 1)
	assert.Equal(t, imagebuild.Request{
		DeploymentId:      "release-dep",
		ApplicationId:     "version-1",
		ApplicationsTable: "dev-appstore-versions",
		SourceUrl:         testSourceUrl,
		Tag:               "v0.3.0",
		Destination:       dest,
		StorageGiB:        100,
	}, builder.requests[0])
	assert.Empty(t, deps.errored)
}

// A top-level build without variants applies to the single image.
func TestAddToAppstore_TopLevelBuild(t *testing.T) {
	store := &fakeVersionStore{builds: store_dynamodb.VersionBuilds{
		Build: &store_dynamodb.BuildSettings{Args: map[string]string{"A": "1"}, Target: "runtime", Dockerfile: "docker/Dockerfile"},
	}}
	builder := &fakeBuilder{}
	require.NoError(t, addToAppstore(t, store, builder, &fakeDeployments{}, 0, true))

	require.Len(t, builder.requests, 1)
	r := builder.requests[0]
	assert.Empty(t, r.Variant)
	assert.Equal(t, releaseDestination(t, store), r.Destination)
	assert.Equal(t, map[string]string{"A": "1"}, r.BuildArgs)
	assert.Equal(t, "runtime", r.Target)
	assert.Equal(t, "docker/Dockerfile", r.Dockerfile)
}

func dynasdBuilds() store_dynamodb.VersionBuilds {
	return store_dynamodb.VersionBuilds{
		DefaultVariant: "cpu",
		VariantOrder:   []string{"gpu", "cpu", "arm"},
		Variants: map[string]store_dynamodb.VariantBuild{
			"cpu": {DeploymentId: "release-dep", Build: &store_dynamodb.BuildSettings{Args: map[string]string{"TORCH_FLAVOUR": "cpu"}}},
			"gpu": {DeploymentId: "dep-gpu", BuildStorageGiB: 100, Build: &store_dynamodb.BuildSettings{Args: map[string]string{"TORCH_FLAVOUR": "cuda"}, Target: "gpu"}},
			// not built (APP_VARIANTS_ENABLED=false when registered): no deployment
			"arm": {},
		},
	}
}

// Every variant with a deployment is built: the default first, with the
// version's tag; the others with <tag>--<id>, each its own deployment.
func TestAddToAppstore_EveryVariant(t *testing.T) {
	store, builder, deps := &fakeVersionStore{builds: dynasdBuilds()}, &fakeBuilder{}, &fakeDeployments{}
	require.NoError(t, addToAppstore(t, store, builder, deps, 0, true))

	dest := releaseDestination(t, store)
	require.Len(t, builder.requests, 2)
	cpu, gpu := builder.requests[0], builder.requests[1]
	assert.Equal(t, "cpu", cpu.Variant)
	assert.True(t, cpu.DefaultVariant)
	assert.Equal(t, "release-dep", cpu.DeploymentId)
	assert.Equal(t, dest, cpu.Destination)
	assert.Equal(t, map[string]string{"TORCH_FLAVOUR": "cpu"}, cpu.BuildArgs)

	assert.Equal(t, "gpu", gpu.Variant)
	assert.False(t, gpu.DefaultVariant)
	assert.Equal(t, "dep-gpu", gpu.DeploymentId)
	assert.Equal(t, dest+"--gpu", gpu.Destination)
	assert.Equal(t, int32(100), gpu.StorageGiB)
	assert.Equal(t, "gpu", gpu.Target)

	assert.Equal(t, []store_dynamodb.VariantUpdate{{DestinationUrl: dest}}, store.variants["cpu"])
	assert.Equal(t, []store_dynamodb.VariantUpdate{{DestinationUrl: dest + "--gpu", Status: "deploying"}}, store.variants["gpu"])
	assert.Empty(t, store.variants["arm"])
	assert.Empty(t, deps.errored)
}

// A non-default variant that doesn't start fails alone: the version and the
// default variant are untouched.
func TestAddToAppstore_OtherVariantFailsAlone(t *testing.T) {
	store := &fakeVersionStore{builds: dynasdBuilds()}
	builder := &fakeBuilder{fail: map[string]error{"gpu": errors.New("build arg X has a line break in its value")}}
	deps := &fakeDeployments{}
	require.NoError(t, addToAppstore(t, store, builder, deps, 0, true))

	assert.Equal(t, []string{"deploying", "deploying"}, store.statuses, "the version is still deploying")
	gpu := store.variants["gpu"]
	require.Len(t, gpu, 2)
	assert.Equal(t, store_dynamodb.VariantUpdate{Status: "error", FailureReason: "build arg X has a line break in its value"}, gpu[1])
	assert.Equal(t, []string{"dep-gpu"}, deps.errored)
}

// The default variant not starting fails the version, as a failed start
// always did; the other variants are then not built.
func TestAddToAppstore_DefaultVariantFails(t *testing.T) {
	store := &fakeVersionStore{builds: dynasdBuilds()}
	builder := &fakeBuilder{fail: map[string]error{"cpu": errors.New("no capacity")}}
	deps := &fakeDeployments{}
	err := addToAppstore(t, store, builder, deps, 0, true)
	assert.EqualError(t, err, "no capacity")

	require.Len(t, builder.requests, 1)
	assert.Equal(t, "error", store.variants["gpu"][0].Status)
	assert.Equal(t, []string{"dep-gpu"}, deps.errored)
}

func TestAddToAppstore_UnreadableBuildsFail(t *testing.T) {
	store := &fakeVersionStore{getBuildsErr: errors.New("throttled")}
	builder := &fakeBuilder{}
	assert.Error(t, addToAppstore(t, store, builder, &fakeDeployments{}, 0, true))
	assert.Empty(t, builder.requests)
}

func TestPlanAppStoreBuilds(t *testing.T) {
	// the default isn't first in variantOrder; variants missing from the
	// order come after it, by id; storage of the default: the larger
	vb := dynasdBuilds()
	vb.VariantOrder = []string{"gpu", "cpu"}
	vb.Variants["extra"] = store_dynamodb.VariantBuild{DeploymentId: "dep-extra"}
	builds := planAppStoreBuilds(vb, "release-dep", 20)
	ids := []string{}
	for _, b := range builds {
		ids = append(ids, b.Variant)
	}
	assert.Equal(t, []string{"cpu", "gpu", "extra"}, ids)
	assert.Equal(t, int32(20), builds[0].StorageGiB)

	// a default that isn't recorded: the one image, as without variants
	builds = planAppStoreBuilds(store_dynamodb.VersionBuilds{DefaultVariant: "cpu"}, "release-dep", 0)
	assert.Equal(t, []appStoreBuild{{Default: true, DeploymentId: "release-dep"}}, builds)
}

func tags(in []types.Tag) map[string]string {
	m := map[string]string{}
	for _, t := range in {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

func TestKanikoRunTaskInput(t *testing.T) {
	env := map[string]string{"DEPLOYER_TASK_DEF_ARN": "td", "CLUSTER_ARN": "c", "SUBNET_IDS": "a,b", "SECURITY_GROUP": "sg", "DEPLOYER_TASK_DEF_CONTAINER_NAME": "deployer"}
	getenv := func(k string) string { return env[k] }
	base := imagebuild.Request{DeploymentId: "d", ApplicationId: "v", ApplicationsTable: "versions", Destination: testEcrRepo + ":1-v1"}

	// without variants or settings: today's command and tags
	in := kanikoRunTaskInput(base, "git://github.com/org/app.git#refs/tags/v1", nil, getenv)
	assert.Equal(t, []string{"--context", "git://github.com/org/app.git#refs/tags/v1", "--destination", testEcrRepo + ":1-v1", "--force"}, in.Overrides.ContainerOverrides[0].Command)
	assert.Equal(t, map[string]string{"DeploymentId": "d", "ApplicationId": "v", "ApplicationsTable": "versions"}, tags(in.Tags))
	assert.Nil(t, in.Overrides.EphemeralStorage)
	assert.Equal(t, []string{"a", "b"}, in.NetworkConfiguration.AwsvpcConfiguration.Subnets)

	gpu := base
	gpu.Variant, gpu.Destination, gpu.StorageGiB = "gpu", base.Destination+"--gpu", 100
	gpu.BuildArgs, gpu.Target, gpu.Dockerfile = map[string]string{"TORCH_FLAVOUR": "cuda"}, "gpu", "docker/Dockerfile"
	in = kanikoRunTaskInput(gpu, "ctx", nil, getenv)
	assert.Equal(t, []string{"--context", "ctx", "--destination", testEcrRepo + ":1-v1--gpu", "--force",
		"--build-arg", "TORCH_FLAVOUR=cuda", "--target", "gpu", "--dockerfile", "docker/Dockerfile"}, in.Overrides.ContainerOverrides[0].Command)
	assert.Equal(t, "gpu", tags(in.Tags)["Variant"])
	assert.Equal(t, "false", tags(in.Tags)["VariantDefault"])
	assert.Equal(t, int32(100), in.Overrides.EphemeralStorage.SizeInGiB)
}
