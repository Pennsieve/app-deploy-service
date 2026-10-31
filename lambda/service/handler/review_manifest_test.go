package handler

import (
	"testing"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
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

func TestAppReviewRecord(t *testing.T) {
	head := "application: {name: a}\ninputs:\n  - name: fcs\n  - name: meta\nparameters:\n  - name: N\noutputs:\n  - name: out\n"
	cases := []struct {
		name, review string
		want         *store_dynamodb.AppReview
	}{
		{"no review block", "", nil},
		{"every source", `review:
  version: 1
  generate:
    image: r
    command: Rscript tests/fixture.R "$REVIEW_SOURCE_DIR" "$REVIEW_INPUT_DIR"
    root: linked
    source: {doi: 10.5281/zenodo.15388817, files: ["*.fcs"]}
  inputs:
    fcs: {files: [tests/fixtures/a.fcs]}
    meta: {dataset: {url: "https://zenodo.org/records/1/files/meta.csv"}}
  parameters:
    N: 2000
`, &store_dynamodb.AppReview{
			Version: 1,
			Generate: &store_dynamodb.ReviewGenerate{Image: "r", Command: `Rscript tests/fixture.R "$REVIEW_SOURCE_DIR" "$REVIEW_INPUT_DIR"`, Root: "linked",
				Source: &store_dynamodb.ReviewDataset{DOI: "10.5281/zenodo.15388817", Files: []string{"*.fcs"}}},
			Inputs: map[string]store_dynamodb.ReviewInput{
				"fcs":  {Files: []string{"tests/fixtures/a.fcs"}},
				"meta": {Dataset: &store_dynamodb.ReviewDataset{URL: "https://zenodo.org/records/1/files/meta.csv"}},
			},
			Parameters: map[string]string{"N": "2000"},
		}},
		{"a port's generate", "review:\n  version: 1\n  inputs:\n    fcs: {generate: {command: make fixture}}\n",
			&store_dynamodb.AppReview{Version: 1, Inputs: map[string]store_dynamodb.ReviewInput{
				"fcs": {Generate: &store_dynamodb.ReviewGenerate{Command: "make fixture"}}}}},
		{"malformed parts are left out", `review:
  version: one
  generate: python make_fixture.py
  inputs:
    fcs: {files: tests/a.fcs}
    meta: {files: [meta.csv]}
  parameters:
    N: [1, 2]
`, &store_dynamodb.AppReview{Inputs: map[string]store_dynamodb.ReviewInput{"meta": {Files: []string{"meta.csv"}}}}},
		{"not a mapping", "review: [1]\n", nil},
	}
	for _, c := range cases {
		cfg, err := parseAppConfig([]byte(head + c.review))
		require.NoError(t, err, "%s: a malformed review never fails app.yml", c.name)
		assert.Equal(t, c.want, appReviewRecord(cfg.Review), c.name)
		// the rest of app.yml is read as always
		assert.Equal(t, "a", cfg.Application.Name, c.name)
		assert.Len(t, cfg.Inputs, 2, c.name)
	}
}

// A malformed review is reported by the schema; reviewProblems still checks
// the names it can read.
func TestReviewProblemsOfAMalformedBlock(t *testing.T) {
	yml := []byte("application: {name: a}\ninputs:\n  - name: fcs\nparameters:\n  - name: N\n" +
		"review:\n  version: 1\n  inputs:\n    fsc: {files: x}\n  parameters:\n    SEED: [1]\n    N: {a: 1}\n")
	cfg, err := parseAppConfig(yml)
	require.NoError(t, err)
	assert.Equal(t, []string{`/review/inputs/fsc: there is no input "fsc"`, `/review/parameters/SEED: there is no parameter "SEED"`},
		reviewProblems(cfg))
	ps := manifestProblems(yml)
	assert.Contains(t, ps, `/review/inputs/fsc: there is no input "fsc"`)
	assert.Greater(t, len(ps), 2, "the schema's problems too: %v", ps)
}
