package mappers

import (
	"testing"

	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
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
