package utils_test

import (
	"fmt"
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
