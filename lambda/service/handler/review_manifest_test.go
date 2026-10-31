package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewProblems(t *testing.T) {
	base := `application: {name: a}
parameters:
  - name: COMPENSATE
    validValues: [auto, "true", "false"]
  - name: N
    type: integer
    min: 0
    max: 100000
inputs:
  - name: fcs
outputs:
  - name: fcs
  - name: metadata
review:
  version: 1
`
	cases := []struct {
		name, review string
		want         []string // empty: no problems
	}{
		{"no more than a version", "", nil},
		{"the app's own names", "  inputs:\n    fcs: {files: [a.fcs]}\n  parameters:\n    COMPENSATE: \"false\"\n    N: 2000\n", nil},
		{"a number written differently", "  parameters:\n    N: 2000.0\n", nil},
		{"an unknown input", "  inputs:\n    fsc: {files: [a.fcs]}\n", []string{`/review/inputs/fsc: there is no input "fsc"`}},
		{"an unknown parameter", "  parameters:\n    SEED: 1\n", []string{`/review/parameters/SEED: there is no parameter "SEED"`}},
		{"a value outside validValues", "  parameters:\n    COMPENSATE: maybe\n", []string{`/review/parameters/COMPENSATE: "maybe" is not one of`}},
		{"below min", "  parameters:\n    N: -1\n", []string{"/review/parameters/N: -1 is less than the parameter's min 0"}},
		{"above max", "  parameters:\n    N: 200000\n", []string{"/review/parameters/N: 200000 is more than the parameter's max 100000"}},
		{"not a number", "  parameters:\n    N: many\n", []string{`/review/parameters/N: "many" is not a number`}},
		{"a list, which the schema reports", "  parameters:\n    N: [1, 2]\n", nil},
	}
	for _, c := range cases {
		cfg, err := parseAppConfig([]byte(base + c.review))
		require.NoError(t, err, c.name)
		got := reviewProblems(cfg)
		if len(c.want) == 0 {
			assert.Empty(t, got, c.name)
			continue
		}
		require.Len(t, got, len(c.want), "%s: %v", c.name, got)
		for i, w := range c.want {
			assert.Contains(t, got[i], w, c.name)
		}
	}

	// no review block: nothing to check
	cfg, err := parseAppConfig([]byte("application: {name: a}\n"))
	require.NoError(t, err)
	assert.Nil(t, cfg.Review)
	assert.Empty(t, reviewProblems(cfg))

	// manifestProblems reports them with the schema's
	got := manifestProblems([]byte(base + "  inputs:\n    fsc: {files: [a.fcs]}\n"))
	assert.Equal(t, []string{`/review/inputs/fsc: there is no input "fsc"`}, got)
}
