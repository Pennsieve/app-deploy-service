package utils

import (
	"fmt"
	"net/url"
	"strings"
)

func ExtractGitUrl(uri string) string {
	u, _ := url.Parse(uri)
	return fmt.Sprintf("%s%s", u.Host, u.Path)
}

func ExtractRepoName(uri string) string {
	return uri[strings.LastIndex(uri, "/")+1:]
}
