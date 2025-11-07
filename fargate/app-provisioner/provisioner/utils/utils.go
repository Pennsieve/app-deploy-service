package utils

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"regexp"
	"strings"
)

func ExtractGitUrl(uri string) string {
	u, _ := url.Parse(uri)
	return fmt.Sprintf("%s%s", u.Host, u.Path)
}

func ExtractRepoName(uri string) string {
	return strings.ToLower(uri[strings.LastIndex(uri, "/")+1:])
}

func GenerateHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func AppSlug(s string, t string) string {
	sourceUrlComputeNodeSlug := fmt.Sprintf("%s-%v", s, t)
	return fmt.Sprint(GenerateHash(sourceUrlComputeNodeSlug))
}

var ErrTagRequired = errors.New("tag is required for https source URLs")

func DetermineSourceURL(sourceURL string, tag string) (string, error) {
	if matched, _ := regexp.MatchString(`^https?://`, sourceURL); matched {
		if tag == "" {
			return "", errors.New("tag is required for https source URLs")
		}
		newSourceURL := strings.Replace(sourceURL, "https://", "git://", 1)
		return fmt.Sprintf("%s#refs/tags/%s", newSourceURL, tag), nil
	}
	return sourceURL, nil
}
