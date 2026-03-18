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

func TestExtractRouteKeyWithSlash(t *testing.T) {
	got := utils.ExtractRoute("GET /")
	if got != "/" {
		t.Errorf("expected /, got %s", got)
	}
}

func TestExtractRouteKeyEmptyPathNormalizesToSlash(t *testing.T) {
	got := utils.ExtractRoute("GET ")
	if got != "/" {
		t.Errorf("expected /, got %s", got)
	}
}

func TestExtractRouteDefaultRoute(t *testing.T) {
	got := utils.ExtractRoute("$default")
	if got != "$default" {
		t.Errorf("expected $default, got %s", got)
	}
}

func TestMatchRoute(t *testing.T) {
	patterns := []string{
		"/",
		"/{id}",
		"/{id}/deployments",
		"/{id}/deployments/{deploymentId}",
		"/deploy",
		"/store",
		"/store/authorize",
		"/store/{id}/permissions",
	}

	tests := []struct {
		name           string
		path           string
		expectMatch    string
		expectParams   map[string]string
		expectFound    bool
	}{
		{"root", "/", "/", nil, true},
		{"app by id", "/abc-123", "/{id}", map[string]string{"id": "abc-123"}, true},
		{"deployments", "/abc/deployments", "/{id}/deployments", map[string]string{"id": "abc"}, true},
		{"deployment by id", "/abc/deployments/def", "/{id}/deployments/{deploymentId}", map[string]string{"id": "abc", "deploymentId": "def"}, true},
		{"deploy", "/deploy", "/deploy", map[string]string{}, true},
		{"store", "/store", "/store", map[string]string{}, true},
		{"store authorize", "/store/authorize", "/store/authorize", map[string]string{}, true},
		{"store permissions", "/store/abc/permissions", "/store/{id}/permissions", map[string]string{"id": "abc"}, true},
		{"no match", "/unknown/path/here", "", nil, false},
		{"empty path", "", "/", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, params, found := utils.MatchRoute(tt.path, patterns)
			if found != tt.expectFound {
				t.Errorf("expected found=%v, got %v", tt.expectFound, found)
				return
			}
			if !found {
				return
			}
			if matched != tt.expectMatch {
				t.Errorf("expected match=%s, got %s", tt.expectMatch, matched)
			}
			if tt.expectParams != nil {
				for k, v := range tt.expectParams {
					if params[k] != v {
						t.Errorf("expected param %s=%s, got %s", k, v, params[k])
					}
				}
			}
		})
	}
}
