package utils

import (
	"regexp"
	"strings"
)

func ExtractRoute(requestRouteKey string) string {
	if requestRouteKey == "$default" {
		return "$default"
	}
	r := regexp.MustCompile(`(?P<method>) (?P<pathKey>.*)`)
	routeKeyParts := r.FindStringSubmatch(requestRouteKey)
	pathKey := routeKeyParts[r.SubexpIndex("pathKey")]
	if pathKey == "" {
		return "/"
	}
	return pathKey
}

func MatchRoute(path string, patterns []string) (string, map[string]string, bool) {
	if path == "" {
		path = "/"
	}

	var bestPattern string
	var bestParams map[string]string
	bestScore := -1

	for _, pattern := range patterns {
		params, ok := matchPattern(pattern, path)
		if !ok {
			continue
		}
		score := patternSpecificity(pattern)
		if score > bestScore {
			bestScore = score
			bestPattern = pattern
			bestParams = params
		}
	}

	if bestScore < 0 {
		return "", nil, false
	}
	return bestPattern, bestParams, true
}

func matchPattern(pattern, path string) (map[string]string, bool) {
	if pattern == "/" && path == "/" {
		return nil, true
	}

	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return nil, false
	}

	params := map[string]string{}
	for i, pp := range patternParts {
		if strings.HasPrefix(pp, "{") && strings.HasSuffix(pp, "}") {
			paramName := pp[1 : len(pp)-1]
			params[paramName] = pathParts[i]
		} else if pp != pathParts[i] {
			return nil, false
		}
	}
	return params, true
}

func patternSpecificity(pattern string) int {
	parts := strings.Split(strings.Trim(pattern, "/"), "/")
	score := 0
	for _, p := range parts {
		if !strings.HasPrefix(p, "{") {
			score += 2
		} else {
			score += 1
		}
	}
	return score
}
