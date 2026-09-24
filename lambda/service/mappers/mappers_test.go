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
