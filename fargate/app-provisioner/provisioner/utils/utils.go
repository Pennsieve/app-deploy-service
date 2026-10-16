package utils

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"regexp"
	"strings"
)

func ExtractGitUrl(uri string) string {
	u, _ := url.Parse(uri)
	return fmt.Sprintf("%s%s", u.Host, u.Path)
}

func ExtractRepoName(uri string) string {
	return strings.ToLower(uri[strings.LastIndex(uri, "/")+1:])
}

func GenerateHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func AppSlug(s string, t string) string {
	sourceUrlComputeNodeSlug := fmt.Sprintf("%s-%v", s, t)
	return fmt.Sprint(GenerateHash(sourceUrlComputeNodeSlug))
}

var ErrTagRequired = errors.New("tag is required for https source URLs")
var ErrRefRequired = errors.New("ref is required for dev builds")
var ErrCommitRequired = errors.New("commit is required for dev builds")
var ErrUnknownRefType = errors.New("unknown ref type for dev build")

const (
	ChannelDev    = "dev"
	RefTypeBranch = "branch"
	RefTypeTag    = "tag"
	RefTypeCommit = "commit"
)

type SourceRef struct {
	Tag     string
	Ref     string
	RefType string
	Commit  string
	Channel string
}

func (r SourceRef) IsDev() bool {
	return r.Channel == ChannelDev
}

func DetermineSourceURL(sourceURL string, tag string) (string, error) {
	return DetermineSourceURLForRef(sourceURL, SourceRef{Tag: tag})
}

func DetermineSourceURLForRef(sourceURL string, ref SourceRef) (string, error) {
	if matched, _ := regexp.MatchString(`^https?://`, sourceURL); !matched {
		return sourceURL, nil
	}
	gitURL := strings.Replace(sourceURL, "https://", "git://", 1)

	if !ref.IsDev() {
		if ref.Tag == "" {
			return "", ErrTagRequired
		}
		return fmt.Sprintf("%s#refs/tags/%s", gitURL, ref.Tag), nil
	}

	switch ref.RefType {
	case RefTypeCommit:
		if ref.Commit == "" {
			return "", ErrCommitRequired
		}
		return fmt.Sprintf("%s#%s", gitURL, ref.Commit), nil
	case RefTypeBranch:
		if ref.Ref == "" {
			return "", ErrRefRequired
		}
		if ref.Commit == "" {
			return fmt.Sprintf("%s#refs/heads/%s", gitURL, ref.Ref), nil
		}
		return fmt.Sprintf("%s#refs/heads/%s#%s", gitURL, ref.Ref, ref.Commit), nil
	case RefTypeTag:
		if ref.Ref == "" {
			return "", ErrRefRequired
		}
		if ref.Commit == "" {
			return fmt.Sprintf("%s#refs/tags/%s", gitURL, ref.Ref), nil
		}
		return fmt.Sprintf("%s#refs/tags/%s#%s", gitURL, ref.Ref, ref.Commit), nil
	default:
		return "", ErrUnknownRefType
	}
}

func ShortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

var invalidTagChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func SanitizeTagComponent(s string, maxLen int) string {
	s = invalidTagChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if maxLen > 0 && len(s) > maxLen {
		s = strings.Trim(s[:maxLen], "-.")
	}
	return s
}

// ImageTag returns the ECR image tag for a build. Release builds keep the
// historical {hash}-{tag} form. Dev builds are prefixed with "dev-" so a
// lifecycle policy can expire them, and carry the short commit plus a build
// suffix so repeated builds never collide in an immutable repository.
func ImageTag(sourceURL string, ref SourceRef, buildId string) string {
	hash := GenerateHash(sourceURL)
	if !ref.IsDev() {
		return fmt.Sprintf("%d-%s", hash, ref.Tag)
	}
	parts := []string{"dev", fmt.Sprint(hash)}
	if ref.RefType != RefTypeCommit {
		if component := SanitizeTagComponent(ref.Ref, 40); component != "" {
			parts = append(parts, component)
		}
	}
	parts = append(parts, ShortCommit(ref.Commit))
	if suffix := SanitizeTagComponent(buildId, 8); suffix != "" {
		parts = append(parts, suffix)
	}
	return strings.Join(parts, "-")
}
