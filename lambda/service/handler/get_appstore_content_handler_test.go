package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
)

type mockContentAppLookup struct {
	app *store_dynamodb.AppStoreApplication
	err error
}

func (m *mockContentAppLookup) GetById(ctx context.Context, uuid string) (*store_dynamodb.AppStoreApplication, error) {
	return m.app, m.err
}

type mockContentReader struct {
	data        []byte
	contentType string
	err         error
	lastKey     string
}

func (m *mockContentReader) Read(ctx context.Context, key string) ([]byte, string, error) {
	m.lastKey = key
	if m.err != nil {
		return nil, "", m.err
	}
	return m.data, m.contentType, nil
}

func TestGetAppStoreContent_MissingFileParam(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "some-uuid"},
		QueryStringParameters: map[string]string{},
	}

	resp, err := getAppStoreContent(t.Context(), request, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestGetAppStoreContent_AppNotFound(t *testing.T) {
	store := &mockContentAppLookup{app: nil}
	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "nonexistent"},
		QueryStringParameters: map[string]string{"file": "pennsieve.json"},
	}

	resp, err := getAppStoreContent(t.Context(), request, store, nil)
	assert.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestGetAppStoreContent_AppLookupError(t *testing.T) {
	store := &mockContentAppLookup{err: fmt.Errorf("dynamo error")}
	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "some-uuid"},
		QueryStringParameters: map[string]string{"file": "pennsieve.json"},
	}

	resp, err := getAppStoreContent(t.Context(), request, store, nil)
	assert.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
}

func TestGetAppStoreContent_Success(t *testing.T) {
	store := &mockContentAppLookup{
		app: &store_dynamodb.AppStoreApplication{
			Uuid:      "app-123",
			SourceUrl: "https://github.com/org/repo",
		},
	}
	dest := &mockContentReader{
		data:        []byte(`{"name":"test-app"}`),
		contentType: "application/json",
	}

	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "app-123"},
		QueryStringParameters: map[string]string{"file": "pennsieve.json", "tag": "v1.0.0"},
	}

	resp, err := getAppStoreContent(t.Context(), request, store, dest)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, `{"name":"test-app"}`, resp.Body)
	assert.Equal(t, "org/repo/v1.0.0/pennsieve.json", dest.lastKey)
}

func TestGetAppStoreContent_DefaultTag(t *testing.T) {
	store := &mockContentAppLookup{
		app: &store_dynamodb.AppStoreApplication{
			Uuid:      "app-123",
			SourceUrl: "https://github.com/org/repo",
		},
	}
	dest := &mockContentReader{
		data:        []byte("# README"),
		contentType: "text/markdown",
	}

	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "app-123"},
		QueryStringParameters: map[string]string{"file": "README.md"},
	}

	resp, err := getAppStoreContent(t.Context(), request, store, dest)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "org/repo/main/README.md", dest.lastKey)
}

func TestGetAppStoreContent_S3ReadError(t *testing.T) {
	store := &mockContentAppLookup{
		app: &store_dynamodb.AppStoreApplication{
			Uuid:      "app-123",
			SourceUrl: "https://github.com/org/repo",
		},
	}
	dest := &mockContentReader{err: fmt.Errorf("NoSuchKey")}

	request := events.APIGatewayV2HTTPRequest{
		PathParameters:        map[string]string{"id": "app-123"},
		QueryStringParameters: map[string]string{"file": "missing.json", "tag": "v1.0.0"},
	}

	resp, err := getAppStoreContent(t.Context(), request, store, dest)
	assert.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}
