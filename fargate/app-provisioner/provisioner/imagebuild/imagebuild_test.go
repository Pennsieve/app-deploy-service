package imagebuild

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testConfig = Config{
	ProjectName:             "dev-app-deploy-service-image-builder-use1",
	CacheRepoUrl:            "123456789012.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-buildcache-use1",
	GitTokenParameterPrefix: "/dev/app-deploy-service/image-builds",
}

var testRequest = Request{
	DeploymentId:      "deployment-1",
	ApplicationId:     "version-1",
	ApplicationsTable: "dev-appstore-versions-table",
	SourceUrl:         "https://github.com/pennsieve-apps/processor-seizure-dynasd",
	Tag:               "v0.2.0",
	Destination:       "123456789012.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1:1234-v0.2.0",
}

func vars(in *codebuild.StartBuildInput) map[string]cbtypes.EnvironmentVariable {
	m := map[string]cbtypes.EnvironmentVariable{}
	for _, v := range in.EnvironmentVariablesOverride {
		m[aws.ToString(v.Name)] = v
	}
	return m
}

func TestSelected(t *testing.T) {
	for in, want := range map[string]string{"": BuilderKaniko, "kaniko": BuilderKaniko, " CodeBuild ": BuilderCodeBuild, "codebuild": BuilderCodeBuild} {
		got, err := Selected(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := Selected("buildkit")
	assert.Error(t, err)
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{
		ProjectEnv:        testConfig.ProjectName,
		CacheRepoEnv:      testConfig.CacheRepoUrl,
		GitTokenPrefixEnv: testConfig.GitTokenParameterPrefix + "/",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	require.NoError(t, err)
	assert.Equal(t, testConfig, cfg)

	_, err = ConfigFromEnv(func(string) string { return "" })
	require.Error(t, err)
	assert.Contains(t, err.Error(), ProjectEnv)
	assert.Contains(t, err.Error(), CacheRepoEnv)
}

func TestGitContext(t *testing.T) {
	assert.Equal(t, "https://github.com/org/repo.git#refs/tags/v1.0.0", GitContext("https://github.com/org/repo", "v1.0.0"))
	assert.Equal(t, "https://github.com/org/repo.git#refs/tags/v1.0.0", GitContext("https://github.com/org/repo.git", "v1.0.0"))
	assert.Equal(t, "https://github.com/org/repo.git#refs/tags/v1.0.0", GitContext("https://github.com/org/repo/", "v1.0.0"))
	assert.Equal(t, "https://github.com/org/repo.git#refs/tags/v1.0.0", GitContext("git://github.com/org/repo", "v1.0.0"))
}

func TestCacheRefIsPerApp(t *testing.T) {
	a := CacheRef(testConfig.CacheRepoUrl, "https://github.com/org/app-a")
	b := CacheRef(testConfig.CacheRepoUrl, "https://github.com/org/app-b")
	assert.NotEqual(t, a, b)
	assert.Equal(t, a, CacheRef(testConfig.CacheRepoUrl, "https://github.com/org/app-a"), "stable across releases")
	assert.True(t, strings.HasPrefix(a, testConfig.CacheRepoUrl+":app-"))
	assert.Len(t, strings.TrimPrefix(a, testConfig.CacheRepoUrl+":app-"), defaultCacheKeySize)
	assert.NotContains(t, a, "appstore-private", "cache never goes to appstore-private")
}

func TestComputeTypeOverride(t *testing.T) {
	for _, storage := range []int32{0, 20, 100, 128} {
		_, ok := ComputeTypeOverride(storage)
		assert.False(t, ok, "storage %d fits BUILD_GENERAL1_LARGE", storage)
	}
	ct, ok := ComputeTypeOverride(200)
	assert.True(t, ok)
	assert.Equal(t, cbtypes.ComputeTypeBuildGeneral1Xlarge, ct)
}

func TestStartBuildInput_PublicRepo(t *testing.T) {
	in := StartBuildInput(testConfig, testRequest, "")

	assert.Equal(t, testConfig.ProjectName, aws.ToString(in.ProjectName))
	assert.Equal(t, testRequest.DeploymentId, aws.ToString(in.IdempotencyToken))
	assert.Empty(t, in.ComputeTypeOverride, "GPU apps (100 GiB) fit the default compute type")

	v := vars(in)
	assert.Equal(t, "https://github.com/pennsieve-apps/processor-seizure-dynasd.git#refs/tags/v0.2.0", aws.ToString(v[ContextVar].Value))
	assert.Equal(t, testRequest.Destination, aws.ToString(v[DestinationVar].Value))
	assert.Equal(t, CacheRef(testConfig.CacheRepoUrl, testRequest.SourceUrl), aws.ToString(v[CacheRefVar].Value))
	assert.Equal(t, testRequest.DeploymentId, aws.ToString(v[DeploymentIdVar].Value))
	assert.Equal(t, testRequest.ApplicationId, aws.ToString(v[ApplicationIdVar].Value))
	assert.Equal(t, testRequest.ApplicationsTable, aws.ToString(v[ApplicationsTableVar].Value))
	_, hasToken := v[GitTokenVar]
	assert.False(t, hasToken)
	for name, ev := range v {
		assert.Equal(t, cbtypes.EnvironmentVariableTypePlaintext, ev.Type, name)
	}
}

func TestStartBuildInput_PrivateRepoTokenIsAParameterReference(t *testing.T) {
	param := GitTokenParameterName(testConfig.GitTokenParameterPrefix, testRequest.DeploymentId)
	in := StartBuildInput(testConfig, testRequest, param)

	token := vars(in)[GitTokenVar]
	assert.Equal(t, cbtypes.EnvironmentVariableTypeParameterStore, token.Type)
	assert.Equal(t, "/dev/app-deploy-service/image-builds/deployment-1/git-token", aws.ToString(token.Value))
}

func TestStartBuildInput_LargeStorage(t *testing.T) {
	r := testRequest
	r.StorageGiB = 200
	assert.Equal(t, cbtypes.ComputeTypeBuildGeneral1Xlarge, StartBuildInput(testConfig, r, "").ComputeTypeOverride)
}

type fakeCodeBuild struct {
	in  *codebuild.StartBuildInput
	err error
}

func (f *fakeCodeBuild) StartBuild(_ context.Context, in *codebuild.StartBuildInput, _ ...func(*codebuild.Options)) (*codebuild.StartBuildOutput, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &codebuild.StartBuildOutput{Build: &cbtypes.Build{Arn: aws.String("arn:aws:codebuild:us-east-1:123456789012:build/p:1")}}, nil
}

type fakeSSM struct {
	put     *ssm.PutParameterInput
	deleted []string
}

func (f *fakeSSM) PutParameter(_ context.Context, in *ssm.PutParameterInput, _ ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	f.put = in
	return &ssm.PutParameterOutput{}, nil
}

func (f *fakeSSM) DeleteParameter(_ context.Context, in *ssm.DeleteParameterInput, _ ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(in.Name))
	return &ssm.DeleteParameterOutput{}, nil
}

func TestStart_PrivateRepo(t *testing.T) {
	cb, params := &fakeCodeBuild{}, &fakeSSM{}
	arn, err := Start(context.Background(), cb, params, testConfig, testRequest, "gho_secret")
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:codebuild:us-east-1:123456789012:build/p:1", arn)

	require.NotNil(t, params.put)
	assert.Equal(t, ssmtypes.ParameterTypeSecureString, params.put.Type)
	assert.Equal(t, "gho_secret", aws.ToString(params.put.Value))
	token := vars(cb.in)[GitTokenVar]
	assert.Equal(t, aws.ToString(params.put.Name), aws.ToString(token.Value))
	for _, v := range cb.in.EnvironmentVariablesOverride {
		assert.NotEqual(t, "gho_secret", aws.ToString(v.Value), "the token itself is never a build variable")
	}
	assert.Empty(t, params.deleted)
}

func TestStart_PublicRepoStoresNoToken(t *testing.T) {
	cb, params := &fakeCodeBuild{}, &fakeSSM{}
	_, err := Start(context.Background(), cb, params, testConfig, testRequest, "")
	require.NoError(t, err)
	assert.Nil(t, params.put)
}

func TestStart_FailedStartDeletesToken(t *testing.T) {
	cb, params := &fakeCodeBuild{err: errors.New("AccountLimitExceededException")}, &fakeSSM{}
	_, err := Start(context.Background(), cb, params, testConfig, testRequest, "gho_secret")
	require.Error(t, err)
	assert.Equal(t, []string{aws.ToString(params.put.Name)}, params.deleted)
}

// An app without variants or build settings gets exactly the variables it
// got before variants: no VARIANT, BUILD_ARGS, BUILD_TARGET, DOCKERFILE or
// second cache.
func TestStartBuildInput_NoVariantNoSettingsUnchanged(t *testing.T) {
	v := vars(StartBuildInput(testConfig, testRequest, ""))
	names := make([]string, 0, len(v))
	for name := range v {
		names = append(names, name)
	}
	assert.ElementsMatch(t, []string{ContextVar, DestinationVar, CacheRefVar, DeploymentIdVar, ApplicationIdVar, ApplicationsTableVar}, names)
	assert.Equal(t, CacheRef(testConfig.CacheRepoUrl, testRequest.SourceUrl), aws.ToString(v[CacheRefVar].Value))
	assert.Empty(t, KanikoFlags(testRequest))
}

func TestStartBuildInput_DefaultVariant(t *testing.T) {
	r := testRequest
	r.Variant, r.DefaultVariant = "cpu", true
	r.BuildArgs = map[string]string{"TORCH_FLAVOUR": "cpu", "DYNASD_COMMIT": "48b2b95"}
	v := vars(StartBuildInput(testConfig, r, ""))

	assert.Equal(t, CacheRef(testConfig.CacheRepoUrl, r.SourceUrl), aws.ToString(v[CacheRefVar].Value), "the default keeps the app's cache")
	_, second := v[CacheImportRefVar]
	assert.False(t, second)
	assert.Equal(t, "cpu", aws.ToString(v[VariantVar].Value))
	assert.Equal(t, "true", aws.ToString(v[VariantDefaultVar].Value))
	assert.Equal(t, "DYNASD_COMMIT=48b2b95\nTORCH_FLAVOUR=cpu", aws.ToString(v[BuildArgsVar].Value), "sorted by name")
	_, hasTarget := v[BuildTargetVar]
	assert.False(t, hasTarget)
}

func TestStartBuildInput_OtherVariant(t *testing.T) {
	r := testRequest
	r.Variant = "gpu"
	r.Destination = VariantDestination(testRequest.Destination, "gpu", false)
	r.BuildArgs = map[string]string{"TORCH_FLAVOUR": "cuda"}
	r.Target = "gpu"
	r.Dockerfile = "docker/Dockerfile.gpu"
	v := vars(StartBuildInput(testConfig, r, ""))

	appCache := CacheRef(testConfig.CacheRepoUrl, r.SourceUrl)
	assert.Equal(t, appCache+"--gpu", aws.ToString(v[CacheRefVar].Value), "its own cache, keyed by the variant")
	assert.Equal(t, appCache, aws.ToString(v[CacheImportRefVar].Value), "and the default's to import")
	assert.Equal(t, testRequest.Destination+"--gpu", aws.ToString(v[DestinationVar].Value))
	assert.Equal(t, "gpu", aws.ToString(v[VariantVar].Value))
	assert.Equal(t, "false", aws.ToString(v[VariantDefaultVar].Value))
	assert.Equal(t, "TORCH_FLAVOUR=cuda", aws.ToString(v[BuildArgsVar].Value))
	assert.Equal(t, "gpu", aws.ToString(v[BuildTargetVar].Value))
	assert.Equal(t, "docker/Dockerfile.gpu", aws.ToString(v[DockerfileVar].Value))
}

func TestVariantDestination(t *testing.T) {
	base := "123.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-private-use1:1234-v0.3.0"
	assert.Equal(t, base, VariantDestination(base, "", false))
	assert.Equal(t, base, VariantDestination(base, "cpu", true))
	assert.Equal(t, base+"--gpu", VariantDestination(base, "gpu", false))
}

func TestKanikoFlags(t *testing.T) {
	r := Request{BuildArgs: map[string]string{"B": "2", "A": "x y"}, Target: "gpu", Dockerfile: "docker/Dockerfile.gpu"}
	assert.Equal(t, []string{"--build-arg", "A=x y", "--build-arg", "B=2", "--target", "gpu", "--dockerfile", "docker/Dockerfile.gpu"}, KanikoFlags(r))
}

func TestRequestValidate(t *testing.T) {
	assert.NoError(t, testRequest.Validate())
	ok := Request{BuildArgs: map[string]string{"TORCH_FLAVOUR": "cuda 12.1"}, Target: "gpu", Dockerfile: "docker/Dockerfile"}
	assert.NoError(t, ok.Validate())

	for name, r := range map[string]Request{
		"arg name":         {BuildArgs: map[string]string{"1BAD": "x"}},
		"newline in value": {BuildArgs: map[string]string{"A": "x\nB=y"}},
		"target spaces":    {Target: "a b"},
		"absolute":         {Dockerfile: "/etc/Dockerfile"},
		"parent":           {Dockerfile: "../Dockerfile"},
	} {
		assert.Error(t, r.Validate(), name)
	}
}

func TestStart_InvalidSettingsStartNothing(t *testing.T) {
	cb, params := &fakeCodeBuild{}, &fakeSSM{}
	r := testRequest
	r.BuildArgs = map[string]string{"A": "x\ny"}
	_, err := Start(context.Background(), cb, params, testConfig, r, "token")
	assert.Error(t, err)
	assert.Nil(t, cb.in)
	assert.Empty(t, params.put)
}
