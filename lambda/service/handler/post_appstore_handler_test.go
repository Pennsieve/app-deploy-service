package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/app-deploy-service/service/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const revisionTestSha = "0123456789abcdef0123456789abcdef01234567"

func appStoreRequest(t *testing.T, deployment models.AppStoreDeployment) events.APIGatewayV2HTTPRequest {
	t.Helper()
	body, err := json.Marshal(deployment)
	require.NoError(t, err)
	return events.APIGatewayV2HTTPRequest{Body: string(body)}
}

func revisionDeployment(refType, ref, commit string) models.AppStoreDeployment {
	return models.AppStoreDeployment{Source: models.DeploymentSource{
		SourceType: "github",
		Url:        "https://github.com/owner/repo",
		Revision:   &models.Revision{RefType: refType, Ref: ref, Commit: commit},
	}}
}

func TestPostAppStoreHandler_BadBody(t *testing.T) {
	resp, err := PostAppStoreHandler(t.Context(), events.APIGatewayV2HTTPRequest{Body: `{not-json`})
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestPostAppStoreHandler_RevisionValidation(t *testing.T) {
	tests := []struct {
		name       string
		deployment models.AppStoreDeployment
		wantErr    error
	}{
		{"release without tag", models.AppStoreDeployment{Source: models.DeploymentSource{SourceType: "github", Url: "https://github.com/owner/repo"}}, models.ErrReleaseTagRequired},
		{"missing commit", revisionDeployment(models.RefTypeBranch, "main", ""), models.ErrRevisionCommitRequired},
		{"missing ref", revisionDeployment(models.RefTypeBranch, "", revisionTestSha), models.ErrRevisionRefRequired},
		{"bad ref type", revisionDeployment("pull", "1", revisionTestSha), models.ErrRevisionRefTypeInvalid},
		{"latest is blocked", revisionDeployment(models.RefTypeBranch, "latest", revisionTestSha), models.ErrRevisionRefDisallowed},
		{"malformed commit", revisionDeployment(models.RefTypeCommit, "nothex!", "nothex!"), models.ErrRevisionCommitInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := PostAppStoreHandler(t.Context(), appStoreRequest(t, tt.deployment))
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var body models.ApplicationResponse
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.Equal(t, tt.wantErr.Error(), body.Message)
		})
	}
}

func TestPostAppStoreHandler_RevisionRejectsRelease(t *testing.T) {
	deployment := revisionDeployment(models.RefTypeCommit, revisionTestSha, revisionTestSha)
	deployment.Release = models.Release{ID: 99}

	resp, err := PostAppStoreHandler(t.Context(), appStoreRequest(t, deployment))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, resp.Body, models.ErrRevisionWithRelease.Error())
}
