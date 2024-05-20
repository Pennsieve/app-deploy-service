package utils

import (
	"fmt"
	"net/url"
)

func ExtractGitUrl(uri string) string {
	u, _ := url.Parse(uri)
	return fmt.Sprintf("%s%s", u.Host, u.Path)
}
