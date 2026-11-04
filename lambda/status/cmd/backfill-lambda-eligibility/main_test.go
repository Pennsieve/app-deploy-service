package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseVersions(t *testing.T) {
	assert.Nil(t, parseVersions(""))
	assert.Nil(t, parseVersions(" , ,"))
	assert.Equal(t, []string{"a1"}, parseVersions("a1"))
	assert.Equal(t, []string{"a1", "b2"}, parseVersions(" a1, b2 ,,a1"))
}
