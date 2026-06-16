package handler

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func TestPostAppStoreSyncHandler_BadBody(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		Body: `{not-json`,
	}

	resp, err := PostAppStoreSyncHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
}

func TestPostAppStoreSyncHandler_MissingSourceUrl(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		Body: `{"source":{"isPrivate":true}}`,
	}

	resp, err := PostAppStoreSyncHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}
