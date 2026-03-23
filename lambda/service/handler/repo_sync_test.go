package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	github "github.com/pennsieve/github-client/pkg/github"
	ghsync "github.com/pennsieve/github-client/pkg/github/sync"
	"github.com/stretchr/testify/assert"
)

func TestBuildNamespace(t *testing.T) {
	tests := []struct {
		name      string
		sourceUrl string
		tag       string
		expected  string
	}{
		{
			name:      "standard github URL",
			sourceUrl: "https://github.com/org/repo",
			tag:       "v1.0.0",
			expected:  "org/repo/v1.0.0",
		},
		{
			name:      "github URL with trailing slash",
			sourceUrl: "https://github.com/org/repo/",
			tag:       "main",
			expected:  "org/repo/main",
		},
		{
			name:      "github URL with .git suffix",
			sourceUrl: "https://github.com/org/repo.git",
			tag:       "v2.0.0",
			expected:  "org/repo/v2.0.0",
		},
		{
			name:      "short URL with fewer than 2 parts",
			sourceUrl: "repo",
			tag:       "main",
			expected:  "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildNamespace(tt.sourceUrl, tt.tag)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyncRepoContent_NoBucket(t *testing.T) {
	t.Setenv("CONTENT_SYNC_BUCKET", "")
	syncRepoContent(t.Context(), "https://github.com/org/repo", "main", "")
}

type mockGitHubApi struct {
	github.GitHubApi
	contentMap map[string]*github.GitHubContentResponse
	err        error
}

func (m *mockGitHubApi) GetContent(url string, filePath string, tag string) (*github.GitHubContentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := fmt.Sprintf("%s/%s/%s", url, filePath, tag)
	resp, ok := m.contentMap[key]
	if !ok {
		return nil, nil
	}
	return resp, nil
}

func TestGitHubContentFetcher_GetContent(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	mock := &mockGitHubApi{
		contentMap: map[string]*github.GitHubContentResponse{
			"https://github.com/org/repo/README.md/main": {
				Content:  encoded,
				Encoding: "base64",
			},
		},
	}
	fetcher := &gitHubContentFetcher{client: mock}

	resp, err := fetcher.GetContent("https://github.com/org/repo", "README.md", "main")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, encoded, resp.Content)
	assert.Equal(t, "base64", resp.Encoding)
}

func TestGitHubContentFetcher_GetContent_NotFound(t *testing.T) {
	mock := &mockGitHubApi{contentMap: map[string]*github.GitHubContentResponse{}}
	fetcher := &gitHubContentFetcher{client: mock}

	resp, err := fetcher.GetContent("https://github.com/org/repo", "missing.txt", "main")
	assert.NoError(t, err)
	assert.Nil(t, resp)
}

func TestGitHubContentFetcher_GetContent_Error(t *testing.T) {
	mock := &mockGitHubApi{err: fmt.Errorf("api error")}
	fetcher := &gitHubContentFetcher{client: mock}

	resp, err := fetcher.GetContent("https://github.com/org/repo", "README.md", "main")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

type mockDestination struct {
	written map[string][]byte
}

func (m *mockDestination) Write(ctx context.Context, key string, data []byte, contentType string) error {
	m.written[key] = data
	return nil
}

func (m *mockDestination) Read(ctx context.Context, key string) ([]byte, string, error) {
	return nil, "", nil
}

func TestSyncContent_Integration(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"name":"test-app"}`))
	readmeEncoded := base64.StdEncoding.EncodeToString([]byte("# Test App"))

	mock := &mockGitHubApi{
		contentMap: map[string]*github.GitHubContentResponse{
			"https://github.com/org/repo/pennsieve.json/v1.0.0": {
				Content:  encoded,
				Encoding: "base64",
			},
			"https://github.com/org/repo/README.md/v1.0.0": {
				Content:  readmeEncoded,
				Encoding: "base64",
			},
		},
	}
	fetcher := &gitHubContentFetcher{client: mock}
	dest := &mockDestination{written: make(map[string][]byte)}

	config := ghsync.Config{
		RepoUrl:   "https://github.com/org/repo",
		Tag:       "v1.0.0",
		Namespace: "org/repo/v1.0.0",
		Files:     []string{"pennsieve.json", "README.md"},
	}

	results := ghsync.SyncContent(t.Context(), logger, fetcher, config, dest)
	for _, r := range results {
		assert.NoError(t, r.Error)
	}

	assert.Equal(t, []byte(`{"name":"test-app"}`), dest.written["org/repo/v1.0.0/pennsieve.json"])
	assert.Equal(t, []byte("# Test App"), dest.written["org/repo/v1.0.0/README.md"])
}
