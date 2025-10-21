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

func TestDetermineSourceURL(t *testing.T) {
	sourceURL := "https://github.com/owner/repo"
	tag := "v1.0.0"
	expected := "git://github.com/owner/repo#refs/tags/v1.0.0"
	got, _ := utils.DetermineSourceURL(sourceURL, tag)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}

	sourceURL = "https://github.com/owner/repo"
	tag = ""
	got, err := utils.DetermineSourceURL(sourceURL, tag)
	if err.Error() != utils.ErrTagRequired.Error() {
		t.Errorf("expected to get error: %s, got nil instead", utils.ErrTagRequired)
	}

	sourceURL = "git://github.com/owner/repo"
	tag = "v1.0.0"
	expected = "git://github.com/owner/repo"
	got, _ = utils.DetermineSourceURL(sourceURL, tag)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}

}
