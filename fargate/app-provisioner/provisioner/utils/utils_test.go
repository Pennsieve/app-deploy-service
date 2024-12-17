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
	assert.Equal(t, "cytof-pipeline-2267669434", fmt.Sprint(result))
}
