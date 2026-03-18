package utils

import (
	"regexp"
)

func ExtractRoute(requestRouteKey string) string {
	r := regexp.MustCompile(`(?P<method>) (?P<pathKey>.*)`)
	routeKeyParts := r.FindStringSubmatch(requestRouteKey)
	if len(routeKeyParts) == 0 {
		return "/"
	}
	pathKey := routeKeyParts[r.SubexpIndex("pathKey")]
	if pathKey == "" {
		return "/"
	}
	return pathKey
}
