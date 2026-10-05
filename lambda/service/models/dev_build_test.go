package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const testSha = "0123456789abcdef0123456789abcdef01234567"

func devSource(refType, ref, commit string) DeploymentSource {
	return DeploymentSource{SourceType: "github", Url: "https://github.com/owner/repo", Channel: ChannelDev, RefType: refType, Ref: ref, Commit: commit}
}

func TestValidateDevBuild(t *testing.T) {
	tests := []struct {
		name   string
		source DeploymentSource
		err    error
	}{
		{"release build skips validation", DeploymentSource{Tag: "v1.0.0"}, nil},
		{"commit", devSource(RefTypeCommit, testSha, testSha), nil},
		{"short commit", devSource(RefTypeCommit, "0123456", "0123456"), nil},
		{"branch", devSource(RefTypeBranch, "feature/x", testSha), nil},
		{"tag", devSource(RefTypeTag, "v2.0.0-rc1", testSha), nil},
		{"unknown ref type", devSource("pr", "1", testSha), ErrDevRefTypeInvalid},
		{"missing ref type", devSource("", "main", testSha), ErrDevRefTypeInvalid},
		{"missing ref", devSource(RefTypeBranch, "", testSha), ErrDevRefRequired},
		{"latest branch", devSource(RefTypeBranch, "latest", testSha), ErrDevRefDisallowed},
		{"latest tag any case", devSource(RefTypeTag, "LATEST", testSha), ErrDevRefDisallowed},
		{"missing commit", devSource(RefTypeBranch, "main", ""), ErrDevCommitRequired},
		{"commit too short", devSource(RefTypeCommit, "abc", "abc"), ErrDevCommitInvalid},
		{"commit not hex", devSource(RefTypeCommit, "zzzzzzzz", "zzzzzzzz"), ErrDevCommitInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, tt.source.ValidateDevBuild(), tt.err)
		})
	}
}

func TestAppStoreDeploymentValidate(t *testing.T) {
	assert.NoError(t, AppStoreDeployment{Source: DeploymentSource{Tag: "v1.0.0"}, Release: Release{ID: 12}}.Validate())
	assert.NoError(t, AppStoreDeployment{Source: devSource(RefTypeCommit, testSha, testSha)}.Validate())
	assert.ErrorIs(t, AppStoreDeployment{Source: devSource(RefTypeCommit, testSha, testSha), Release: Release{ID: 12}}.Validate(), ErrDevReleaseNotEmpty)
	assert.ErrorIs(t, AppStoreDeployment{Source: devSource(RefTypeCommit, testSha, "")}.Validate(), ErrDevCommitRequired)
}

func TestVersionLabel(t *testing.T) {
	assert.Equal(t, "v1.0.0", DeploymentSource{Tag: "v1.0.0"}.VersionLabel())
	assert.Equal(t, "dev-0123456", devSource(RefTypeCommit, testSha, testSha).VersionLabel())
	assert.Equal(t, "dev-0123456", devSource(RefTypeCommit, testSha, "0123456789ABCDEF").VersionLabel())
	assert.Equal(t, "dev-feature-x", devSource(RefTypeBranch, "feature/x", testSha).VersionLabel())
	assert.Equal(t, "dev-main", devSource(RefTypeBranch, "main", testSha).VersionLabel())
	assert.Equal(t, "dev-v2.0.0-rc1", devSource(RefTypeTag, "v2.0.0-rc1", testSha).VersionLabel())
}

func TestContentRef(t *testing.T) {
	assert.Equal(t, "v1.0.0", DeploymentSource{Tag: "v1.0.0"}.ContentRef())
	assert.Equal(t, testSha, devSource(RefTypeBranch, "main", testSha).ContentRef())
}

func TestIsDevVersion(t *testing.T) {
	assert.True(t, IsDevVersion(AppStoreVersion{Channel: ChannelDev}))
	assert.False(t, IsDevVersion(AppStoreVersion{Version: "v1.0.0"}))
}
