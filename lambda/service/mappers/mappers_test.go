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
