package utils_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
	"github.com/stretchr/testify/assert"
)

func TestExtractGitUrl(t *testing.T) {
	uri := "git://github.com/edmore/cytof-pipeline"
	expected := "github.com/edmore/cytof-pipeline"
	got := utils.ExtractGitUrl(uri)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestExtractRepoName(t *testing.T) {
	uri := "git://github.com/edmore/cytof-PIPELINE"
	expected := "cytof-pipeline"
	got := utils.ExtractRepoName(uri)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestGenerateHash(t *testing.T) {
	s := "fs-0795cd79bc369ed00"
	result := utils.GenerateHash(s)
	assert.Equal(t, "1641283831", fmt.Sprint(result))
}

func TestAppSlug(t *testing.T) {
	s := "git://github.com/edmore/cytof-PIPELINE"
	s2 := "f03bea87-c766-4ec3-96b9-519fa31c1de9"
	result := utils.AppSlug(s, s2)
	assert.Equal(t, "3672999531", fmt.Sprint(result))
}

func TestUniqueAppIdentifierPerOrg(t *testing.T) {
	sourceUrl1 := "git://github.com/test/test-PIPELINE"
	sourceUrl2 := "git://github.com/test2/test-PIPELINE"
	uuid := "f03bea87-c766-4ec3-96b9-519fa31c1de9"

	result := utils.AppSlug(sourceUrl1, uuid)
	result2 := utils.AppSlug(sourceUrl2, uuid)
	assert.NotEqual(t, result, result2)
}

func TestDetermineSourceURL(t *testing.T) {
	sourceURL := "https://github.com/owner/repo"
	tag := "v1.0.0"
	expected := "git://github.com/owner/repo#refs/tags/v1.0.0"
	got, _ := utils.DetermineSourceURL(sourceURL, tag)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}

	sourceURL = "https://github.com/owner/repo"
	tag = ""
	got, err := utils.DetermineSourceURL(sourceURL, tag)
	if err.Error() != utils.ErrTagRequired.Error() {
		t.Errorf("expected to get error: %s, got nil instead", utils.ErrTagRequired)
	}

	sourceURL = "git://github.com/owner/repo"
	tag = "v1.0.0"
	expected = "git://github.com/owner/repo"
	got, _ = utils.DetermineSourceURL(sourceURL, tag)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}

}

func TestDetermineSourceURLForRef(t *testing.T) {
	sourceURL := "https://github.com/owner/repo"
	sha := "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name     string
		ref      utils.SourceRef
		expected string
		err      error
	}{
		{"release tag", utils.SourceRef{Tag: "v1.0.0"}, "git://github.com/owner/repo#refs/tags/v1.0.0", nil},
		{"release missing tag", utils.SourceRef{}, "", utils.ErrTagRequired},
		{"dev commit", utils.SourceRef{Channel: "dev", RefType: "commit", Ref: sha, Commit: sha}, "git://github.com/owner/repo#" + sha, nil},
		{"dev commit missing commit", utils.SourceRef{Channel: "dev", RefType: "commit"}, "", utils.ErrCommitRequired},
		{"dev branch pinned", utils.SourceRef{Channel: "dev", RefType: "branch", Ref: "feature/x", Commit: sha}, "git://github.com/owner/repo#refs/heads/feature/x#" + sha, nil},
		{"dev branch unpinned", utils.SourceRef{Channel: "dev", RefType: "branch", Ref: "main"}, "git://github.com/owner/repo#refs/heads/main", nil},
		{"dev branch missing ref", utils.SourceRef{Channel: "dev", RefType: "branch", Commit: sha}, "", utils.ErrRefRequired},
		{"dev tag pinned", utils.SourceRef{Channel: "dev", RefType: "tag", Ref: "v2.0.0-rc1", Commit: sha}, "git://github.com/owner/repo#refs/tags/v2.0.0-rc1#" + sha, nil},
		{"dev tag unpinned", utils.SourceRef{Channel: "dev", RefType: "tag", Ref: "v2.0.0-rc1"}, "git://github.com/owner/repo#refs/tags/v2.0.0-rc1", nil},
		{"dev unknown ref type", utils.SourceRef{Channel: "dev", RefType: "pr", Ref: "1", Commit: sha}, "", utils.ErrUnknownRefType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.DetermineSourceURLForRef(sourceURL, tt.ref)
			assert.ErrorIs(t, err, tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}

	t.Run("non-https source is returned untouched", func(t *testing.T) {
		got, err := utils.DetermineSourceURLForRef("git://github.com/owner/repo", utils.SourceRef{Channel: "dev", RefType: "commit", Commit: sha})
		assert.NoError(t, err)
		assert.Equal(t, "git://github.com/owner/repo", got)
	})
}

func TestSanitizeTagComponent(t *testing.T) {
	assert.Equal(t, "feature-x", utils.SanitizeTagComponent("feature/x", 0))
	assert.Equal(t, "fix-bug-123", utils.SanitizeTagComponent("fix/bug#123", 0))
	assert.Equal(t, "release_1.2", utils.SanitizeTagComponent("release_1.2", 0))
	assert.Equal(t, "abc", utils.SanitizeTagComponent("--abc..", 0))
	assert.Equal(t, "abcd", utils.SanitizeTagComponent("abcdefgh", 4))
	assert.Equal(t, "", utils.SanitizeTagComponent("///", 0))
}

func TestImageTag(t *testing.T) {
	sourceURL := "https://github.com/owner/repo"
	hash := fmt.Sprint(utils.GenerateHash(sourceURL))
	sha := "0123456789abcdef0123456789abcdef01234567"
	buildId := "9f8e7d6c-1234-5678-9abc-def012345678"

	assert.Equal(t, hash+"-v1.0.0", utils.ImageTag(sourceURL, utils.SourceRef{Tag: "v1.0.0"}, buildId))
	assert.Equal(t, "dev-"+hash+"-0123456-9f8e7d6c", utils.ImageTag(sourceURL, utils.SourceRef{Channel: "dev", RefType: "commit", Ref: sha, Commit: sha}, buildId))
	assert.Equal(t, "dev-"+hash+"-feature-x-0123456-9f8e7d6c", utils.ImageTag(sourceURL, utils.SourceRef{Channel: "dev", RefType: "branch", Ref: "feature/x", Commit: sha}, buildId))
	assert.Equal(t, "dev-"+hash+"-v2.0.0-rc1-0123456-9f8e7d6c", utils.ImageTag(sourceURL, utils.SourceRef{Channel: "dev", RefType: "tag", Ref: "v2.0.0-rc1", Commit: sha}, buildId))
	assert.Equal(t, "dev-"+hash+"-0123456", utils.ImageTag(sourceURL, utils.SourceRef{Channel: "dev", RefType: "commit", Commit: sha}, ""))

	tag := utils.ImageTag(sourceURL, utils.SourceRef{Channel: "dev", RefType: "branch", Ref: strings.Repeat("very-long-branch-name/", 10), Commit: sha}, buildId)
	assert.LessOrEqual(t, len(tag), 128)
	assert.Regexp(t, `^[A-Za-z0-9_][A-Za-z0-9_.-]*$`, tag)
}
