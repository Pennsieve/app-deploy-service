package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func newRequest(method, routeKey, rawPath string, pathParams map[string]string) events.APIGatewayV2HTTPRequest {
	return events.APIGatewayV2HTTPRequest{
		RouteKey:       routeKey,
		RawPath:        rawPath,
		PathParameters: pathParams,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			RequestID: "handler-test",
			AccountID: "12345",
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: method,
			},
		},
	}
}

func stubHandler(_ context.Context, _ events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: "ok"}, nil
}

func newTestRouter() Router {
	router := NewLambdaRouter()
	router.POST("/", stubHandler)
	router.GET("/", stubHandler)
	router.GET("/{id}", stubHandler)
	router.GET("/{id}/deployments", stubHandler)
	router.GET("/{id}/deployments/{deploymentId}", stubHandler)
	router.DELETE("/{id}", stubHandler)
	router.PUT("/{id}", stubHandler)
	router.POST("/deploy", stubHandler)
	router.POST("/store", stubHandler)
	router.GET("/store", stubHandler)
	router.GET("/store/registry", stubHandler)
	router.GET("/store/{id}", stubHandler)
	router.GET("/store/{id}/permissions", stubHandler)
	router.PUT("/store/{id}/permissions", stubHandler)
	return router
}

func TestUnknownRouteReturnsNotFound(t *testing.T) {
	request := newRequest("POST", "POST /unknownEndpoint", "/unknownEndpoint", nil)
	resp, _ := AppDeployServiceHandler(context.Background(), request)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, ErrUnsupportedRoute.Error(), resp.Body)
}

func TestUnsupportedMethodReturnsUnprocessableEntity(t *testing.T) {
	request := newRequest("PATCH", "PATCH /", "/", nil)
	resp, _ := AppDeployServiceHandler(context.Background(), request)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Equal(t, ErrUnsupportedPath.Error(), resp.Body)
}

func TestRouteMatching(t *testing.T) {
	router := newTestRouter()

	tests := []struct {
		name     string
		method   string
		routeKey string
		rawPath  string
		params   map[string]string
	}{
		// root routes: /applications/ (with trailing slash)
		{"POST root with slash", "POST", "POST /", "/", nil},
		{"GET root with slash", "GET", "GET /", "/", nil},
		// root routes: /applications (without trailing slash) - normalized to "/"
		{"POST root without slash", "POST", "POST ", "", nil},
		{"GET root without slash", "GET", "GET ", "", nil},

		// application routes
		{"GET app by id", "GET", "GET /{id}", "/123", map[string]string{"id": "123"}},
		{"DELETE app by id", "DELETE", "DELETE /{id}", "/123", map[string]string{"id": "123"}},
		{"PUT app by id", "PUT", "PUT /{id}", "/123", map[string]string{"id": "123"}},

		// deployment routes
		{"GET deployments", "GET", "GET /{id}/deployments", "/123/deployments", map[string]string{"id": "123"}},
		{"GET deployment by id", "GET", "GET /{id}/deployments/{deploymentId}", "/123/deployments/456", map[string]string{"id": "123", "deploymentId": "456"}},

		// deploy route
		{"POST deploy", "POST", "POST /deploy", "/deploy", nil},

		// appstore routes
		{"POST store", "POST", "POST /store", "/store", nil},
		{"GET store", "GET", "GET /store", "/store", nil},
		{"GET store registry", "GET", "GET /store/registry", "/store/registry", nil},

		// appstore application detail route
		{"GET store app by id", "GET", "GET /store/{id}", "/store/123", map[string]string{"id": "123"}},

		// appstore permission routes
		{"GET store permissions", "GET", "GET /store/{id}/permissions", "/store/123/permissions", map[string]string{"id": "123"}},
		{"PUT store permissions", "PUT", "PUT /store/{id}/permissions", "/store/123/permissions", map[string]string{"id": "123"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := newRequest(tt.method, tt.routeKey, tt.rawPath, tt.params)
			resp, err := router.Start(context.Background(), request)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode, "route should match and return OK")
			assert.Equal(t, "ok", resp.Body)
		})
	}
}

func TestUnknownRoutePerMethod(t *testing.T) {
	router := newTestRouter()

	tests := []struct {
		name     string
		method   string
		routeKey string
	}{
		{"GET unknown", "GET", "GET /nonexistent"},
		{"POST unknown", "POST", "POST /nonexistent"},
		{"DELETE unknown", "DELETE", "DELETE /nonexistent"},
		{"PUT unknown", "PUT", "PUT /nonexistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := newRequest(tt.method, tt.routeKey, "/nonexistent", nil)
			resp, _ := router.Start(context.Background(), request)
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.Equal(t, ErrUnsupportedRoute.Error(), resp.Body)
		})
	}
}
