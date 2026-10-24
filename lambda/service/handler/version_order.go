package handler

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

// Which of an app's versions is the newest: by semantic version when the
// tags are ones ("v1.2.0", "1.10.0-rc1"), else by when they were registered.
// Registering an older tag again makes it the most recent registration, not
// the newest version.

type taggedVersion struct {
	Tag       string
	CreatedAt string
}

// parseSemver reads "v1.2.3" or "1.2.3-rc1" into its numbers and pre-release;
// ok is false for anything else.
func parseSemver(tag string) (nums [3]int, pre string, ok bool) {
	t := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if i := strings.IndexByte(t, '+'); i >= 0 {
		t = t[:i]
	}
	if i := strings.IndexByte(t, '-'); i >= 0 {
		t, pre = t[:i], t[i+1:]
	}
	parts := strings.Split(t, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// compareSemver orders two parsed versions; a pre-release sorts before its
// release.
func compareSemver(a [3]int, aPre string, b [3]int, bPre string) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case aPre == bPre:
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	case aPre < bPre:
		return -1
	default:
		return 1
	}
}

// newestTag is the newest version's tag: the highest semantic version when
// every tag is one, else the most recently registered. "" without tags.
func newestTag(versions []taggedVersion) string {
	allSemver := true
	n := 0
	for _, v := range versions {
		if v.Tag == "" {
			continue
		}
		n++
		if _, _, ok := parseSemver(v.Tag); !ok {
			allSemver = false
		}
	}
	if n == 0 {
		return ""
	}
	best := taggedVersion{}
	for _, v := range versions {
		if v.Tag == "" {
			continue
		}
		if best.Tag == "" {
			best = v
			continue
		}
		if allSemver {
			a, ap, _ := parseSemver(v.Tag)
			b, bp, _ := parseSemver(best.Tag)
			if c := compareSemver(a, ap, b, bp); c > 0 || (c == 0 && v.CreatedAt > best.CreatedAt) {
				best = v
			}
		} else if v.CreatedAt > best.CreatedAt {
			best = v
		}
	}
	return best.Tag
}

// isNewestVersion reports whether tag is the app's newest version, counting
// the one just registered. On a lookup failure it says yes, as before
// versions were ordered.
func isNewestVersion(ctx context.Context, versions store_dynamodb.AppStoreVersionDBStore, applicationId, tag string) bool {
	all, err := versions.GetByApplicationId(ctx, applicationId)
	if err != nil {
		log.Printf("warning: unable to list the versions of %s: %v", applicationId, err)
		return true
	}
	tagged := make([]taggedVersion, 0, len(all))
	for _, v := range all {
		tagged = append(tagged, taggedVersion{Tag: v.Version, CreatedAt: v.CreatedAt})
	}
	newest := newestTag(tagged)
	return newest == "" || newest == tag
}
