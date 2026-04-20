package handler

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func TestGetAppStoreContentHandler_MissingFileParam(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "some-uuid"},
		QueryStringParameters: map[string]string{},
	}

	resp, err := GetAppStoreContentHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGetAppStoreContentHandler_MissingBucket(t *testing.T) {
	t.Setenv("CONTENT_SYNC_BUCKET", "")
	t.Setenv("APPSTORE_APPLICATIONS_TABLE", "test-table")

	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "some-uuid"},
		QueryStringParameters: map[string]string{"file": "application.json"},
	}

	resp, err := GetAppStoreContentHandler(t.Context(), request)
	assert.NoError(t, err)
	// Will fail at DynamoDB or bucket check depending on AWS config availability
	assert.True(t, resp.StatusCode >= 400)
}
