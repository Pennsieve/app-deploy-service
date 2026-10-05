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

	disallowedRef = "latest"
)

var (
	ErrRevisionRefRequired    = errors.New("revision builds require a ref")
	ErrRevisionRefTypeInvalid = errors.New("revision builds require refType of branch, tag, or commit")
	ErrRevisionCommitRequired = errors.New("revision builds require a resolved commit sha")
	ErrRevisionCommitInvalid  = errors.New("revision commit must be a 7 to 40 character hex sha")
	ErrRevisionRefDisallowed  = errors.New("revision builds cannot target the ref 'latest'")
	ErrRevisionWithRelease    = errors.New("revision builds cannot reference a release")
	ErrReleaseTagRequired     = errors.New("release builds require a tag")
)

var commitShaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
var invalidVersionChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// IsRevisionBuild reports whether the source asks for a build of a git revision
// rather than a release. The
// presence of the "revision" object is the signal; release builds omit it.
func (s DeploymentSource) IsRevisionBuild() bool {
	return s.Revision != nil
}

// Channel is the value stored on the version record and the prefix used for
// version labels and image tags. Revision builds land on the dev channel;
// releases have none.
func (s DeploymentSource) Channel() string {
	if s.IsRevisionBuild() {
		return ChannelDev
	}
	return ""
}

func (s DeploymentSource) ValidateRevision() error {
	if !s.IsRevisionBuild() {
		return nil
	}
	return s.Revision.Validate()
}

func (r Revision) Validate() error {
	switch r.RefType {
	case RefTypeBranch, RefTypeTag, RefTypeCommit:
	default:
		return ErrRevisionRefTypeInvalid
	}
	if r.Ref == "" {
		return ErrRevisionRefRequired
	}
	if strings.EqualFold(r.Ref, disallowedRef) {
		return ErrRevisionRefDisallowed
	}
	if r.Commit == "" {
		return ErrRevisionCommitRequired
	}
	if !commitShaPattern.MatchString(strings.ToLower(r.Commit)) {
		return ErrRevisionCommitInvalid
	}
	return nil
}

func (a AppStoreDeployment) Validate() error {
	if !a.Source.IsRevisionBuild() {
		if strings.TrimSpace(a.Source.Tag) == "" {
			return ErrReleaseTagRequired
		}
		return nil
	}
	if err := a.Source.ValidateRevision(); err != nil {
		return err
	}
	if a.Release.ID != 0 {
		return ErrRevisionWithRelease
	}
	return nil
}

// ContentRef is the git ref used to sync repository content (app.yml, README).
// Revision builds pin to the commit so the synced content matches the build exactly.
func (s DeploymentSource) ContentRef() string {
	if s.IsRevisionBuild() {
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
// Release builds keep the tag. Revision builds are prefixed with their channel;
// builds of a branch share one label so repeated builds update the same
// version, while tags and commits get their own.
func (s DeploymentSource) VersionLabel() string {
	if !s.IsRevisionBuild() {
		return s.Tag
	}
	prefix := s.Channel() + "-"
	switch s.Revision.RefType {
	case RefTypeBranch, RefTypeTag:
		return prefix + sanitizeVersionComponent(s.Revision.Ref)
	default:
		return prefix + ShortCommit(strings.ToLower(s.Revision.Commit))
	}
}

func sanitizeVersionComponent(s string) string {
	s = invalidVersionChars.ReplaceAllString(s, "-")
	return strings.Trim(s, "-.")
}

func IsDevVersion(v AppStoreVersion) bool {
	return v.Channel == ChannelDev
}
