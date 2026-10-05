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
	ErrReleaseTagRequired = errors.New("release builds require a tag")
)

var commitShaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
var invalidVersionChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// IsDevBuild reports whether the source asks for a build of a git revision
// rather than a release. The
// presence of the "revision" object is the signal; release builds omit it.
func (s DeploymentSource) IsDevBuild() bool {
	return s.Revision != nil
}

// Channel is the value stored on the version record. Revision builds land on
// the dev channel;
// releases have none.
func (s DeploymentSource) Channel() string {
	if s.IsDevBuild() {
		return ChannelDev
	}
	return ""
}

func (s DeploymentSource) ValidateDevBuild() error {
	if !s.IsDevBuild() {
		return nil
	}
	return s.Revision.Validate()
}

func (r Revision) Validate() error {
	switch r.RefType {
	case RefTypeBranch, RefTypeTag, RefTypeCommit:
	default:
		return ErrDevRefTypeInvalid
	}
	if r.Ref == "" {
		return ErrDevRefRequired
	}
	if strings.EqualFold(r.Ref, disallowedRef) {
		return ErrDevRefDisallowed
	}
	if r.Commit == "" {
		return ErrDevCommitRequired
	}
	if !commitShaPattern.MatchString(strings.ToLower(r.Commit)) {
		return ErrDevCommitInvalid
	}
	return nil
}

func (a AppStoreDeployment) Validate() error {
	if !a.Source.IsDevBuild() {
		if strings.TrimSpace(a.Source.Tag) == "" {
			return ErrReleaseTagRequired
		}
		return nil
	}
	if err := a.Source.ValidateDevBuild(); err != nil {
		return err
	}
	if a.Release.ID != 0 {
		return ErrDevReleaseNotEmpty
	}
	return nil
}

// ContentRef is the git ref used to sync repository content (app.yml, README).
// Revision builds pin to the commit so the synced content matches the build exactly.
func (s DeploymentSource) ContentRef() string {
	if s.IsDevBuild() {
		return s.Revision.Commit
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
// Release builds keep the tag. Revision builds of a branch share one label so
// repeated builds update the same version; tags and commits get their own.
func (s DeploymentSource) VersionLabel() string {
	if !s.IsDevBuild() {
		return s.Tag
	}
	switch s.Revision.RefType {
	case RefTypeBranch, RefTypeTag:
		return devVersionPrefix + sanitizeVersionComponent(s.Revision.Ref)
	default:
		return devVersionPrefix + ShortCommit(strings.ToLower(s.Revision.Commit))
	}
}

func sanitizeVersionComponent(s string) string {
	s = invalidVersionChars.ReplaceAllString(s, "-")
	return strings.Trim(s, "-.")
}

func IsDevVersion(v AppStoreVersion) bool {
	return v.Channel == ChannelDev
}
