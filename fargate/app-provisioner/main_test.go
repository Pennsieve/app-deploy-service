package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/imagebuild"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBuildStorageGiB(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int32
	}{
		{"empty", "", 0},
		{"valid", "100", 100},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"non-numeric", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseBuildStorageGiB(tt.in); got != tt.want {
				t.Errorf("parseBuildStorageGiB(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewBuildStarter(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1"}
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	_, err := newBuildStarter("codebuild", getenv, cfg)
	assert.Error(t, err, "codebuild without its project and cache repo")

	env[imagebuild.ProjectEnv] = "dev-app-deploy-service-image-builder-use1"
	env[imagebuild.CacheRepoEnv] = "123456789012.dkr.ecr.us-east-1.amazonaws.com/dev-appstore-buildcache-use1"
	env[imagebuild.GitTokenPrefixEnv] = "/dev/app-deploy-service/image-builds"
	for _, flag := range []string{"", "codebuild"} {
		b, err := newBuildStarter(flag, getenv, cfg)
		require.NoError(t, err, flag)
		assert.Equal(t, imagebuild.BuilderCodeBuild, b.Name())
		assert.Equal(t, env[imagebuild.ProjectEnv], b.(codeBuildStarter).cfg.ProjectName)
	}

	for _, flag := range []string{"kaniko", "buildkit"} {
		_, err = newBuildStarter(flag, getenv, cfg)
		assert.Error(t, err, "%q is refused, not defaulted", flag)
	}
}
