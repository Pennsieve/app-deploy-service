package utils

import (
	"errors"
	"fmt"
	"regexp"
)

func ExtractRoute(requestRouteKey string) string {
	r := regexp.MustCompile(`(?P<method>) (?P<pathKey>.*)`)
	routeKeyParts := r.FindStringSubmatch(requestRouteKey)
	return routeKeyParts[r.SubexpIndex("pathKey")]
}

var ErrTagRequired = errors.New("tag is required for https source URLs")

func DetermineSourceURL(sourceURL string, tag string) (string, error) {
	if matched, _ := regexp.MatchString(`^https?://`, sourceURL); matched {
		if tag == "" {
			return "", errors.New("tag is required for https source URLs")
		}
		return fmt.Sprintf("%s/archive/refs/tags/%s.tar.gz", sourceURL, tag), nil
	}
	return sourceURL, nil
}
