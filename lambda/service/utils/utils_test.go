package utils_test

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/app-deploy-service/service/utils"
)

func TestExtractRouteKey(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		RouteKey: "POST /applications",
	}
	expected := "/applications"
	got := utils.ExtractRoute(request.RouteKey)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}
