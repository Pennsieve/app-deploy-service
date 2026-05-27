package handler

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func TestPatchAppstoreApplicationHandler_MissingId(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{},
		Body:           `{"status":"archived"}`,
	}

	resp, err := PatchAppstoreApplicationHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestPatchAppstoreApplicationHandler_EmptyId(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{"id": ""},
		Body:           `{"status":"archived"}`,
	}

	resp, err := PatchAppstoreApplicationHandler(t.Context(), request)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}
