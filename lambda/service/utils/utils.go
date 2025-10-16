package utils

import (
	"fmt"
	"regexp"
)

func ExtractRoute(requestRouteKey string) string {
	r := regexp.MustCompile(`(?P<method>) (?P<pathKey>.*)`)
	routeKeyParts := r.FindStringSubmatch(requestRouteKey)
	return routeKeyParts[r.SubexpIndex("pathKey")]
}

func DetermineSourceURL(sourceURL string, tag string) string {
	if matched, _ := regexp.MatchString(`^https?://`, sourceURL); matched {
		return fmt.Sprintf("%s#%s", sourceURL, tag)
	}
	return sourceURL
}
