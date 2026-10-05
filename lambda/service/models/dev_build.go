package models

import (
	"errors"
	"regexp"
	"strings"
)

const (
	ChannelDev    = "dev"
	RefTypeBranch = "branch"
	RefTypeTag    = "tag"
	RefTypeCommit = "commit"

	devVersionPrefix = "dev-"
	disallowedRef    = "latest"
)

var (
	ErrDevRefRequired     = errors.New("dev builds require a ref")
	ErrDevRefTypeInvalid  = errors.New("dev builds require refType of branch, tag, or commit")
	ErrDevCommitRequired  = errors.New("dev builds require a resolved commit sha")
	ErrDevCommitInvalid   = errors.New("dev build commit must be a 7 to 40 character hex sha")
	ErrDevRefDisallowed   = errors.New("dev builds cannot target the ref 'latest'")
	ErrDevReleaseNotEmpty = errors.New("dev builds cannot reference a release")
)

var commitShaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
var invalidVersionChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func (s DeploymentSource) IsDevBuild() bool {
	return s.Channel == ChannelDev
}

func (s DeploymentSource) ValidateDevBuild() error {
	if !s.IsDevBuild() {
		return nil
	}
	switch s.RefType {
	case RefTypeBranch, RefTypeTag, RefTypeCommit:
	default:
		return ErrDevRefTypeInvalid
	}
	if s.Ref == "" {
		return ErrDevRefRequired
	}
	if strings.EqualFold(s.Ref, disallowedRef) {
		return ErrDevRefDisallowed
	}
	if s.Commit == "" {
		return ErrDevCommitRequired
	}
	if !commitShaPattern.MatchString(strings.ToLower(s.Commit)) {
		return ErrDevCommitInvalid
	}
	return nil
}

func (a AppStoreDeployment) Validate() error {
	if err := a.Source.ValidateDevBuild(); err != nil {
		return err
	}
	if a.Source.IsDevBuild() && a.Release.ID != 0 {
		return ErrDevReleaseNotEmpty
	}
	return nil
}

// ContentRef is the git ref used to sync repository content (app.yml, README).
// Dev builds pin to the commit so the synced content matches the build exactly.
func (s DeploymentSource) ContentRef() string {
	if s.IsDevBuild() {
		return s.Commit
	}
	return s.Tag
}

func ShortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// VersionLabel is the value stored in the version record's "version" field.
// Release builds keep the tag. Dev builds of a branch share one label so
// repeated builds update the same version; tags and commits get their own.
func (s DeploymentSource) VersionLabel() string {
	if !s.IsDevBuild() {
		return s.Tag
	}
	switch s.RefType {
	case RefTypeBranch, RefTypeTag:
		return devVersionPrefix + sanitizeVersionComponent(s.Ref)
	default:
		return devVersionPrefix + ShortCommit(strings.ToLower(s.Commit))
	}
}

func sanitizeVersionComponent(s string) string {
	s = invalidVersionChars.ReplaceAllString(s, "-")
	return strings.Trim(s, "-.")
}

func IsDevVersion(v AppStoreVersion) bool {
	return v.Channel == ChannelDev
}
