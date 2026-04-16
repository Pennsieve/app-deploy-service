package handler

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/stretchr/testify/assert"
)

func TestGetAppstoreApplicationHandler_MissingId(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
	}

	resp, err := GetAppstoreApplicationHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGetAppstoreApplicationHandler_EmptyId(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": ""},
	}

	resp, err := GetAppstoreApplicationHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGetAppstoreApplicationHandler_DynamoDBError(t *testing.T) {
	t.Setenv("APPSTORE_APPLICATIONS_TABLE", "nonexistent-table")
	t.Setenv("APPSTORE_VERSIONS_TABLE", "nonexistent-table")
	t.Setenv("DEPLOYMENTS_TABLE", "nonexistent-table")
	t.Setenv("APP_ACCESS_TABLE", "nonexistent-table")

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": "some-uuid"},
	}

	resp, err := GetAppstoreApplicationHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.True(t, resp.StatusCode >= 400)
}

func TestFetchAssets_NoBucket(t *testing.T) {
	t.Setenv("CONTENT_SYNC_BUCKET", "")

	assets := fetchAssets(t.Context(), aws.Config{}, "https://github.com/org/repo", "main")
	assert.Empty(t, assets)
}

func TestFetchAssets_WithBucketNoS3(t *testing.T) {
	t.Setenv("CONTENT_SYNC_BUCKET", "test-bucket")
	t.Setenv("CONTENT_SYNC_FILES", "pennsieve.json,README.md")

	assets := fetchAssets(t.Context(), aws.Config{}, "https://github.com/org/repo", "main")
	// S3 calls will fail, so no assets returned
	assert.Empty(t, assets)
}

func TestFetchAssets_CustomSyncFiles(t *testing.T) {
	t.Setenv("CONTENT_SYNC_BUCKET", "test-bucket")
	t.Setenv("CONTENT_SYNC_FILES", "custom.json")

	assets := fetchAssets(t.Context(), aws.Config{}, "https://github.com/org/repo", "v1.0.0")
	assert.Empty(t, assets)
}

func TestLatestVersionTag_NoVersions(t *testing.T) {
	assert.Equal(t, "", latestVersionTag(nil))
	assert.Equal(t, "", latestVersionTag([]models.AppStoreVersion{}))
}

func TestLatestVersionTag_PicksMostRecentCreatedAt(t *testing.T) {
	versions := []models.AppStoreVersion{
		{Version: "v1.0.0", CreatedAt: "2026-01-01"},
		{Version: "v2.0.0", CreatedAt: "2026-03-15"},
		{Version: "v1.5.0", CreatedAt: "2026-02-10"},
	}
	assert.Equal(t, "v2.0.0", latestVersionTag(versions))
}

func TestLatestVersionTag_SkipsEmptyVersionString(t *testing.T) {
	versions := []models.AppStoreVersion{
		{Version: "", CreatedAt: "2026-04-01"},
		{Version: "v1.0.0", CreatedAt: "2026-01-01"},
	}
	assert.Equal(t, "v1.0.0", latestVersionTag(versions))
}

func TestLatestVersionTag_AllEmptyVersionsReturnsEmpty(t *testing.T) {
	versions := []models.AppStoreVersion{
		{Version: "", CreatedAt: "2026-04-01"},
		{Version: "", CreatedAt: "2026-01-01"},
	}
	assert.Equal(t, "", latestVersionTag(versions))
}

func TestLatestVersionTag_SingleVersion(t *testing.T) {
	versions := []models.AppStoreVersion{
		{Version: "v0.1.0", CreatedAt: "2026-02-01"},
	}
	assert.Equal(t, "v0.1.0", latestVersionTag(versions))
}
