package utils_test

import (
	"testing"

	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/utils"
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
	uri := "git://github.com/edmore/cytof-pipeline"
	expected := "cytof-pipeline"
	got := utils.ExtractRepoName(uri)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}
