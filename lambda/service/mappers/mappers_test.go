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

func TestAppStoreVersionToModelCarriesRevisionFields(t *testing.T) {
	v := store_dynamodb.AppStoreVersion{
		Uuid:          "v-1",
		ApplicationId: "a-1",
		Version:       "dev-feature-x",
		Status:        "deployed",
		Channel:       "dev",
		Ref:           "feature/x",
		RefType:       "branch",
		Commit:        "0123456789abcdef",
	}
	m := AppStoreVersionToModel(v)
	assert.Equal(t, "dev", m.Channel)
	assert.Equal(t, "feature/x", m.Ref)
	assert.Equal(t, "branch", m.RefType)
	assert.Equal(t, "0123456789abcdef", m.Commit)

	release := AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Uuid: "v-2", Version: "v1.0.0"})
	assert.Empty(t, release.Channel)
	assert.Empty(t, release.Commit)
}
