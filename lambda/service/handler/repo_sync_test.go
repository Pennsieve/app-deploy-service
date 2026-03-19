package handler

import (
	"testing"

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
	// should not panic when bucket is not set
	syncRepoContent(t.Context(), "https://github.com/org/repo", "main")
}
