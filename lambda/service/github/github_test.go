package github

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRawURL(t *testing.T) {
	tests := []struct {
		name      string
		sourceUrl string
		filePath  string
		tag       string
		expected  string
		wantErr   bool
	}{
		{
			name:      "standard github URL with default tag",
			sourceUrl: "https://github.com/org/repo",
			filePath:  "pennsieve.json",
			tag:       "",
			expected:  "https://raw.githubusercontent.com/org/repo/main/pennsieve.json",
		},
		{
			name:      "github URL with specific tag",
			sourceUrl: "https://github.com/org/repo",
			filePath:  "README.md",
			tag:       "v1.0.0",
			expected:  "https://raw.githubusercontent.com/org/repo/v1.0.0/README.md",
		},
		{
			name:      "github URL with trailing slash",
			sourceUrl: "https://github.com/org/repo/",
			filePath:  "pennsieve.json",
			tag:       "main",
			expected:  "https://raw.githubusercontent.com/org/repo/main/pennsieve.json",
		},
		{
			name:      "github URL with .git suffix",
			sourceUrl: "https://github.com/org/repo.git",
			filePath:  "pennsieve.json",
			tag:       "main",
			expected:  "https://raw.githubusercontent.com/org/repo/main/pennsieve.json",
		},
		{
			name:      "non-github URL returns error",
			sourceUrl: "https://gitlab.com/org/repo",
			filePath:  "pennsieve.json",
			tag:       "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildRawURL(tt.sourceUrl, tt.filePath, tt.tag)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFetchFileFromRepo_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	content, err := fetchFromURL(server.URL)
	assert.NoError(t, err)
	assert.Nil(t, content)
}

func TestFetchFileFromRepo_Success(t *testing.T) {
	expected := `{"executionTargets":["ecs"],"defaultCPU":2048,"defaultMemory":4096}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expected))
	}))
	defer server.Close()

	content, err := fetchFromURL(server.URL)
	require.NoError(t, err)
	assert.Equal(t, expected, string(content))
}

func TestFetchFileFromRepo_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	content, err := fetchFromURL(server.URL)
	assert.Error(t, err)
	assert.Nil(t, content)
}

func fetchFromURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
