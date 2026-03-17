package github

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func BuildRawURL(sourceUrl, filePath, tag string) (string, error) {
	if tag == "" {
		tag = "main"
	}

	sourceUrl = strings.TrimSuffix(sourceUrl, "/")
	sourceUrl = strings.TrimSuffix(sourceUrl, ".git")

	if !strings.Contains(sourceUrl, "github.com") {
		return "", fmt.Errorf("unsupported source URL: %s", sourceUrl)
	}

	rawUrl := strings.Replace(sourceUrl, "github.com", "raw.githubusercontent.com", 1)
	return fmt.Sprintf("%s/%s/%s", rawUrl, tag, filePath), nil
}

func FetchFileFromRepo(sourceUrl, filePath, tag string) ([]byte, error) {
	rawUrl, err := BuildRawURL(sourceUrl, filePath, tag)
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(rawUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", filePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, filePath)
	}

	return io.ReadAll(resp.Body)
}
