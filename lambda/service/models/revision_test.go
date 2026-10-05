package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

const testSha = "0123456789abcdef0123456789abcdef01234567"

func revisionSource(refType, ref, commit string) DeploymentSource {
	return DeploymentSource{SourceType: "github", Url: "https://github.com/owner/repo", Revision: &Revision{RefType: refType, Ref: ref, Commit: commit}}
}

func TestValidateRevision(t *testing.T) {
	tests := []struct {
		name   string
		source DeploymentSource
		err    error
	}{
		{"release build skips validation", DeploymentSource{Tag: "v1.0.0"}, nil},
		{"empty revision object", DeploymentSource{Revision: &Revision{}}, ErrRevisionRefTypeInvalid},
		{"commit", revisionSource(RefTypeCommit, testSha, testSha), nil},
		{"short commit", revisionSource(RefTypeCommit, "0123456", "0123456"), nil},
		{"branch", revisionSource(RefTypeBranch, "feature/x", testSha), nil},
		{"tag", revisionSource(RefTypeTag, "v2.0.0-rc1", testSha), nil},
		{"unknown ref type", revisionSource("pr", "1", testSha), ErrRevisionRefTypeInvalid},
		{"missing ref type", revisionSource("", "main", testSha), ErrRevisionRefTypeInvalid},
		{"missing ref", revisionSource(RefTypeBranch, "", testSha), ErrRevisionRefRequired},
		{"latest branch", revisionSource(RefTypeBranch, "latest", testSha), ErrRevisionRefDisallowed},
		{"latest tag any case", revisionSource(RefTypeTag, "LATEST", testSha), ErrRevisionRefDisallowed},
		{"missing commit", revisionSource(RefTypeBranch, "main", ""), ErrRevisionCommitRequired},
		{"commit too short", revisionSource(RefTypeCommit, "abc", "abc"), ErrRevisionCommitInvalid},
		{"commit not hex", revisionSource(RefTypeCommit, "zzzzzzzz", "zzzzzzzz"), ErrRevisionCommitInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, tt.source.ValidateRevision(), tt.err)
		})
	}
}

func TestAppStoreDeploymentValidate(t *testing.T) {
	assert.NoError(t, AppStoreDeployment{Source: DeploymentSource{Tag: "v1.0.0"}, Release: Release{ID: 12}}.Validate())
	assert.ErrorIs(t, AppStoreDeployment{Source: DeploymentSource{Url: "https://github.com/o/r"}, Release: Release{ID: 12}}.Validate(), ErrReleaseTagRequired)
	assert.ErrorIs(t, AppStoreDeployment{Source: DeploymentSource{Tag: "  "}}.Validate(), ErrReleaseTagRequired)
	assert.NoError(t, AppStoreDeployment{Source: revisionSource(RefTypeCommit, testSha, testSha)}.Validate())
	assert.ErrorIs(t, AppStoreDeployment{Source: revisionSource(RefTypeCommit, testSha, testSha), Release: Release{ID: 12}}.Validate(), ErrRevisionWithRelease)
	assert.ErrorIs(t, AppStoreDeployment{Source: revisionSource(RefTypeCommit, testSha, "")}.Validate(), ErrRevisionCommitRequired)
}

func TestVersionLabel(t *testing.T) {
	assert.Equal(t, "v1.0.0", DeploymentSource{Tag: "v1.0.0"}.VersionLabel())
	assert.Equal(t, "dev-0123456", revisionSource(RefTypeCommit, testSha, testSha).VersionLabel())
	assert.Equal(t, "dev-0123456", revisionSource(RefTypeCommit, testSha, "0123456789ABCDEF").VersionLabel())
	assert.Equal(t, "dev-feature-x", revisionSource(RefTypeBranch, "feature/x", testSha).VersionLabel())
	assert.Equal(t, "dev-main", revisionSource(RefTypeBranch, "main", testSha).VersionLabel())
	assert.Equal(t, "dev-v2.0.0-rc1", revisionSource(RefTypeTag, "v2.0.0-rc1", testSha).VersionLabel())
}

func TestContentRef(t *testing.T) {
	assert.Equal(t, "v1.0.0", DeploymentSource{Tag: "v1.0.0"}.ContentRef())
	assert.Equal(t, testSha, revisionSource(RefTypeBranch, "main", testSha).ContentRef())
}

func TestIsRevisionBuildAndChannel(t *testing.T) {
	assert.False(t, DeploymentSource{Tag: "v1.0.0"}.IsRevisionBuild())
	assert.Equal(t, "", DeploymentSource{Tag: "v1.0.0"}.Channel())
	assert.True(t, revisionSource(RefTypeBranch, "main", testSha).IsRevisionBuild())
	assert.Equal(t, ChannelDev, revisionSource(RefTypeBranch, "main", testSha).Channel())
}

func TestDeploymentSourceJSON(t *testing.T) {
	var release DeploymentSource
	assert.NoError(t, json.Unmarshal([]byte(`{"type":"github","url":"https://github.com/o/r","tag":"v1.0.0"}`), &release))
	assert.Nil(t, release.Revision)
	assert.False(t, release.IsRevisionBuild())

	var dev DeploymentSource
	assert.NoError(t, json.Unmarshal([]byte(`{"type":"github","url":"https://github.com/o/r","revision":{"ref":"main","refType":"branch","commit":"`+testSha+`"}}`), &dev))
	assert.True(t, dev.IsRevisionBuild())
	assert.Equal(t, "dev-main", dev.VersionLabel())

	out, err := json.Marshal(DeploymentSource{SourceType: "github", Url: "https://github.com/o/r", Tag: "v1.0.0"})
	assert.NoError(t, err)
	assert.NotContains(t, string(out), `"revision"`)
}

func TestIsDevVersion(t *testing.T) {
	assert.True(t, IsDevVersion(AppStoreVersion{Channel: ChannelDev}))
	assert.False(t, IsDevVersion(AppStoreVersion{Version: "v1.0.0"}))
}
