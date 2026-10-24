package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewestTag_Semver(t *testing.T) {
	versions := []taggedVersion{
		{Tag: "v1.10.0", CreatedAt: "2026-01-01"},
		{Tag: "v1.9.0", CreatedAt: "2026-03-01"}, // re-registered later; still older
		{Tag: "v1.10.0-rc1", CreatedAt: "2026-02-01"},
	}
	assert.Equal(t, "v1.10.0", newestTag(versions))
}

func TestNewestTag_PreReleaseBeforeRelease(t *testing.T) {
	assert.Equal(t, "2.0.0", newestTag([]taggedVersion{{Tag: "2.0.0-beta", CreatedAt: "b"}, {Tag: "2.0.0", CreatedAt: "a"}}))
}

func TestNewestTag_FallsBackToRegistration(t *testing.T) {
	versions := []taggedVersion{{Tag: "main", CreatedAt: "2026-01-01"}, {Tag: "v1.0.0", CreatedAt: "2026-02-01"}}
	assert.Equal(t, "v1.0.0", newestTag(versions))
	assert.Equal(t, "", newestTag(nil))
	assert.Equal(t, "", newestTag([]taggedVersion{{Tag: ""}}))
}
