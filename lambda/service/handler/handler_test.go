package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func TestHandler(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		RequestID: "handler-test",
		AccountID: "12345",
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "POST",
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "POST /unknownEndpoint",
		RawPath:        "/unknownEndpoint",
		RequestContext: requestContext,
	}
	resp, _ := AppDeployServiceHandler(context.Background(), request)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, ErrUnsupportedRoute.Error(), resp.Body)
}
