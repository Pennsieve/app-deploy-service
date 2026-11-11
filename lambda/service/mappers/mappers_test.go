package mappers

import (
	"encoding/json"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppStoreAppToModel_DefaultsStatusToActive(t *testing.T) {
	app := store_dynamodb.AppStoreApplication{
		Uuid:      "test-uuid",
		SourceUrl: "https://github.com/test/repo",
		Status:    "",
	}

	result := AppStoreAppToModel(app)
	assert.Equal(t, models.AppStoreStatusActive, result.Status)
}

func TestAppStoreAppToModel_PreservesArchivedStatus(t *testing.T) {
	app := store_dynamodb.AppStoreApplication{
		Uuid:      "test-uuid",
		SourceUrl: "https://github.com/test/repo",
		Status:    "archived",
	}

	result := AppStoreAppToModel(app)
	assert.Equal(t, models.AppStoreStatusArchived, result.Status)
}

func TestAppParametersToModels_PreservesRequired(t *testing.T) {
	params := []store_dynamodb.AppParameter{
		{Name: "consent", Type: "string", Required: true},
		{Name: "accession", Type: "string", Required: false},
		{Name: "tumor", Type: "string", DefaultValue: "No"},
	}

	result := AppParametersToModels(params)
	assert.Equal(t, []models.AppParameter{
		{Name: "consent", Type: "string", Required: true},
		{Name: "accession", Type: "string", Required: false},
		{Name: "tumor", Type: "string", DefaultValue: "No"},
	}, result)
}

func TestAppParametersToModels_Empty(t *testing.T) {
	assert.Nil(t, AppParametersToModels(nil))
	assert.Nil(t, AppParametersToModels([]store_dynamodb.AppParameter{}))
}

func TestAppStoreVersionToModel_Manifest(t *testing.T) {
	v := store_dynamodb.AppStoreVersion{Uuid: "v1", Version: "v1.0.0"}
	assert.Nil(t, AppStoreVersionToModel(v).Manifest, "no app.yml, no manifest")

	v.Manifest = &store_dynamodb.AppManifest{
		Name:    "spike-detect",
		Runtime: store_dynamodb.AppRuntime{CPU: 1024},
		Inputs:  []store_dynamodb.AppPort{{Name: "recording"}},
	}
	m := AppStoreVersionToModel(v).Manifest
	assert.Equal(t, "spike-detect", m.Name)
	assert.Equal(t, 1024, m.Runtime.CPU)
	assert.Equal(t, []models.AppParameter{}, m.Parameters, "empty lists, not null, in JSON")
	assert.Equal(t, []string{}, m.Inputs[0].MediaTypes)
	assert.Equal(t, []models.AppPort{}, m.Outputs)
}

func TestAppStoreVersionToModel_LicenseProblemsMaintainersFormats(t *testing.T) {
	v := store_dynamodb.AppStoreVersion{
		Uuid:             "v1",
		License:          "MIT",
		ManifestProblems: []string{"/runtime: missing property 'kernel'"},
		Manifest: &store_dynamodb.AppManifest{
			Maintainers: []store_dynamodb.AppMaintainer{{Name: "Ada", Orcid: "0000-0002-1825-0097"}},
			Inputs:      []store_dynamodb.AppPort{{Name: "recording", Formats: []string{"format_3590"}}, {Name: "study", ConformsTo: []string{"pennsieve-published-dataset", "bids@1.9"}, Container: "directory"}},
		},
	}
	m := AppStoreVersionToModel(v)
	assert.Equal(t, "MIT", m.License)
	assert.Equal(t, v.ManifestProblems, m.ManifestProblems)
	assert.Equal(t, []models.AppMaintainer{{Name: "Ada", Orcid: "0000-0002-1825-0097"}}, m.Manifest.Maintainers)
	assert.Equal(t, []string{"format_3590"}, m.Manifest.Inputs[0].Formats)
	assert.Equal(t, []string{"pennsieve-published-dataset", "bids@1.9"}, m.Manifest.Inputs[1].ConformsTo)
	assert.Equal(t, "directory", m.Manifest.Inputs[1].Container)
}

func TestAppStoreVersionToModel_Variants(t *testing.T) {
	cpu := &store_dynamodb.AppManifest{Name: "DynaSD", Runtime: store_dynamodb.AppRuntime{ComputeTypes: []string{"standard"}},
		Variant: &store_dynamodb.AppManifestVariant{ID: "cpu", Name: "CPU"}}
	gpu := &store_dynamodb.AppManifest{Name: "DynaSD", Runtime: store_dynamodb.AppRuntime{ComputeTypes: []string{"gpu"}},
		Variant: &store_dynamodb.AppManifestVariant{ID: "gpu", Name: "GPU (ONCET)", Description: "CUDA."}}
	v := store_dynamodb.AppStoreVersion{
		Uuid: "u", Version: "v0.3.0", Status: "deployed", DestinationUrl: "repo:1-v0.3.0", Manifest: cpu,
		DefaultVariant: "cpu", VariantOrder: []string{"gpu", "cpu"},
		Variants: map[string]store_dynamodb.AppVariant{
			"cpu": {Name: "CPU", Manifest: cpu, Build: &store_dynamodb.AppBuild{Args: map[string]string{"TORCH_FLAVOUR": "cpu"}}},
			"gpu": {Name: "GPU (ONCET)", Description: "CUDA.", Manifest: gpu, Status: "error", FailureReason: "no stage", DestinationUrl: "repo:1-v0.3.0--gpu"},
			"arm": {Name: "ARM"},
		},
	}
	m := AppStoreVersionToModel(v)
	assert.Equal(t, "cpu", m.DefaultVariant)
	require.Len(t, m.Variants, 3)
	// variantOrder, then the rest by id
	assert.Equal(t, []string{"gpu", "cpu", "arm"}, []string{m.Variants[0].ID, m.Variants[1].ID, m.Variants[2].ID})

	assert.Equal(t, models.AppStoreVariant{ID: "gpu", Name: "GPU (ONCET)", Description: "CUDA.", Status: "error",
		FailureReason: "no stage", Manifest: AppManifestToModel(gpu)}, m.Variants[0])
	// the default's status is the version's until its own is recorded
	assert.True(t, m.Variants[1].Default)
	assert.Equal(t, "deployed", m.Variants[1].Status)
	// another variant without a build recorded has no status
	assert.False(t, m.Variants[2].Default)
	assert.Empty(t, m.Variants[2].Status)
	assert.Nil(t, m.Variants[2].Manifest)

	assert.Equal(t, &models.AppManifestVariant{ID: "cpu", Name: "CPU"}, m.Manifest.Variant)
	assert.Equal(t, "gpu", m.Variants[0].Manifest.Variant.ID)

	// never the image or build settings
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "repo:")
	assert.NotContains(t, string(b), "TORCH_FLAVOUR")
}

// A version without variants marshals exactly as before: no variant keys.
func TestAppStoreVersionToModel_NoVariantsJSONUnchanged(t *testing.T) {
	m := AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Uuid: "u", Version: "v1", Status: "deployed",
		Manifest: &store_dynamodb.AppManifest{Name: "x"}})
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "variant")

	d := DeploymentItemToModel(store_dynamodb.Deployment{DeploymentKey: store_dynamodb.DeploymentKey{DeploymentId: "d"}})
	b, err = json.Marshal(d)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "variant")

	d = DeploymentItemToModel(store_dynamodb.Deployment{Variant: "gpu"})
	assert.Equal(t, "gpu", d.Variant)
}

func TestAppStoreVersionToModel_ReviewCommitAndDigests(t *testing.T) {
	v := store_dynamodb.AppStoreVersion{
		Uuid: "u", Version: "v1.0.0", CommitSha: "3f786850e387550fdab836ed7e6dc881de23001b", ImageDigest: "sha256:aaa",
		DefaultVariant: "cpu", VariantOrder: []string{"cpu", "gpu"},
		Variants: map[string]store_dynamodb.AppVariant{
			"cpu": {Name: "CPU", ImageDigest: "sha256:aaa"},
			"gpu": {Name: "GPU", ImageDigest: "sha256:bbb"},
		},
		Review: &store_dynamodb.AppReview{
			Version:  1,
			Generate: &store_dynamodb.ReviewGenerate{Command: "make fixture", Root: "linked"},
			Inputs: map[string]store_dynamodb.ReviewInput{
				"fcs": {Dataset: &store_dynamodb.ReviewDataset{DOI: "10.5281/zenodo.15388817", Files: []string{"*.fcs"}}},
			},
			Parameters: map[string]string{"N": "2000"},
		},
	}
	m := AppStoreVersionToModel(v)
	assert.Equal(t, "3f786850e387550fdab836ed7e6dc881de23001b", m.CommitSha)
	assert.Equal(t, "sha256:aaa", m.ImageDigest)
	assert.Equal(t, "sha256:aaa", m.Variants[0].ImageDigest)
	assert.Equal(t, "sha256:bbb", m.Variants[1].ImageDigest)
	assert.Equal(t, &models.AppReview{
		Version:  1,
		Generate: &models.ReviewGenerate{Command: "make fixture", Root: "linked"},
		Inputs: map[string]models.ReviewInput{
			"fcs": {Dataset: &models.ReviewDataset{DOI: "10.5281/zenodo.15388817", Files: []string{"*.fcs"}}},
		},
		Parameters: map[string]string{"N": "2000"},
	}, m.Review)

	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"commitSha":"3f786850e387550fdab836ed7e6dc881de23001b"`)
	assert.Contains(t, string(b), `"review":{"version":1,"generate":{"command":"make fixture","root":"linked"}`)

	// a version registered before these were kept has none of them
	b, err = json.Marshal(AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Uuid: "u"}))
	require.NoError(t, err)
	for _, key := range []string{"review", "commitSha", "imageDigest"} {
		assert.NotContains(t, string(b), key)
	}
}

// The Lambda eligibility the status lambda records, on the version and each
// variant, is in the API as lambda; runtime.optimize is in the manifest.
func TestAppStoreVersionToModel_LambdaEligibility(t *testing.T) {
	optOut := false
	v := store_dynamodb.AppStoreVersion{
		Uuid: "u", DefaultVariant: "cpu", VariantOrder: []string{"cpu", "gpu", "big"},
		Manifest: &store_dynamodb.AppManifest{Runtime: store_dynamodb.AppRuntime{Optimize: &optOut}},
		Lambda:   &store_dynamodb.LambdaEligibility{Eligible: true, ImageBytes: 42, CheckedAt: "2026-11-03T12:00:00Z"},
		Variants: map[string]store_dynamodb.AppVariant{
			"cpu": {Lambda: &store_dynamodb.LambdaEligibility{Eligible: true, ImageBytes: 42, CheckedAt: "2026-11-03T12:00:00Z"}},
			"big": {Lambda: &store_dynamodb.LambdaEligibility{Overridable: true, Reasons: []string{"it needs 16 GB of memory; a Lambda step has at most 10 GB"}, CheckedAt: "2026-11-03T12:00:00Z"}},
			"gpu": {Lambda: &store_dynamodb.LambdaEligibility{Reasons: []string{"it runs on GPU only"}, CheckedAt: "2026-11-03T12:00:00Z"}},
		},
	}
	m := AppStoreVersionToModel(v)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"lambda":{"eligible":true,"imageBytes":42,"checkedAt":"2026-11-03T12:00:00Z"}`)
	assert.Contains(t, string(b), `"lambda":{"eligible":false,"reasons":["it runs on GPU only"],"checkedAt":"2026-11-03T12:00:00Z"}`)
	assert.Contains(t, string(b), `"lambda":{"eligible":false,"overridable":true,"reasons":["it needs 16 GB of memory; a Lambda step has at most 10 GB"],"checkedAt":"2026-11-03T12:00:00Z"}`)
	assert.Contains(t, string(b), `"runtime":{"optimize":false}`)

	assert.Nil(t, AppStoreVersionToModel(store_dynamodb.AppStoreVersion{}).Lambda, "absent until recorded")
}

// The fixture bundle the status lambda records: shown without its bucket,
// key or build id.
func TestAppStoreVersionToModel_Fixtures(t *testing.T) {
	m := AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Uuid: "u", Version: "v1.0.0", Fixtures: &store_dynamodb.AppFixtures{
		Status: "ready", BuildId: "arn:build", Bucket: "dev-fixtures", Key: "fixtures/app/v1.0.0/abc.tar.gz",
		Bytes: 2256444, Sha256: "abc", Files: 7, Warnings: []string{"large_bundle: 300000000 bytes"}, BuiltAt: "2026-11-11T19:59:00Z", UpdatedAt: "2026-11-11T20:00:00Z",
	}})
	require.NotNil(t, m.Fixtures)
	assert.Equal(t, "ready", m.Fixtures.Status)
	assert.Equal(t, int64(2256444), m.Fixtures.Bytes)
	assert.Equal(t, "abc", m.Fixtures.Sha256)
	assert.Equal(t, 7, m.Fixtures.Files)
	assert.Equal(t, []string{"large_bundle: 300000000 bytes"}, m.Fixtures.Warnings)
	assert.Equal(t, "2026-11-11T19:59:00Z", m.Fixtures.BuiltAt)
	assert.Nil(t, m.Fixtures.Error)

	skipped := AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Fixtures: &store_dynamodb.AppFixtures{
		Status: "skipped", Error: &store_dynamodb.AppFixturesError{Code: "no_review_block", Message: "app.yml declares no review block"},
	}})
	require.NotNil(t, skipped.Fixtures.Error)
	assert.Equal(t, "no_review_block", skipped.Fixtures.Error.Code)

	assert.Nil(t, AppStoreVersionToModel(store_dynamodb.AppStoreVersion{}).Fixtures)
}

// The sample run and the sample document the status lambda records: shown
// without the results' location, the build or the job.
func TestAppStoreVersionToModel_SampleRunAndSample(t *testing.T) {
	exit := 0
	m := AppStoreVersionToModel(store_dynamodb.AppStoreVersion{
		SampleRun: &store_dynamodb.AppSampleRun{Status: "ready", BuildId: "arn:b", Bucket: "fx", Key: "samples/app/v1/r.zip", Bytes: 551680,
			Files: 11, ExitCode: &exit, DurationSeconds: 8, ComputeType: "BUILD_GENERAL1_MEDIUM", FinishedAt: "2026-11-11T21:43:57Z"},
		Sample: &store_dynamodb.AppSample{Status: "ready", JobId: "sample-1", Bucket: "cur", DocumentKey: "examples/apps/app/v1/abc.pdf",
			PreviewKey: "examples/apps/app/v1/abc.preview.png", CrateKey: "examples/apps/app/v1/abc.ro-crate-metadata.json",
			Sha256: "abc", Bytes: 120000, Pages: 6, Figures: 3, Narrative: true, Public: map[string]string{"document": "public/apps/app/v1/abc.pdf"},
			GeneratedAt: "2026-11-11T22:00:00Z"},
	})
	require.NotNil(t, m.SampleRun)
	assert.Equal(t, "ready", m.SampleRun.Status)
	assert.Equal(t, 11, m.SampleRun.Files)
	assert.Equal(t, 8, m.SampleRun.DurationSeconds)
	require.NotNil(t, m.SampleRun.ExitCode)
	assert.Equal(t, 0, *m.SampleRun.ExitCode)
	require.NotNil(t, m.Sample)
	assert.Equal(t, "examples/apps/app/v1/abc.pdf", m.Sample.DocumentKey)
	assert.Equal(t, 6, m.Sample.Pages)
	assert.Equal(t, map[string]string{"document": "public/apps/app/v1/abc.pdf"}, m.Sample.Public)
	assert.Nil(t, m.Sample.Error)

	failed := AppStoreVersionToModel(store_dynamodb.AppStoreVersion{
		SampleRun: &store_dynamodb.AppSampleRun{Status: "skipped", Error: &store_dynamodb.AppFixturesError{Code: "needs_hardware"}},
		Sample:    &store_dynamodb.AppSample{Status: "failed", Error: &store_dynamodb.AppFixturesError{Code: "no_outputs", Message: "nothing written"}},
	})
	assert.Equal(t, "needs_hardware", failed.SampleRun.Error.Code)
	assert.Equal(t, "no_outputs", failed.Sample.Error.Code)
	assert.Nil(t, AppStoreVersionToModel(store_dynamodb.AppStoreVersion{}).Sample)
}
