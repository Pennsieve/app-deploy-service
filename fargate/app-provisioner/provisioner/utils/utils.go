package utils

import (
	"fmt"
	"hash/fnv"
	"net/url"
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
	return fmt.Sprintf("%s-%v", ExtractRepoName(s), GenerateHash(t))
}
