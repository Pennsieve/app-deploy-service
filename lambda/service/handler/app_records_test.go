package handler

import (
	"strings"
	"testing"
)

// cohortStats takes a dataset's files and its metadata records.
const cohortStats = `schemaVersion: "1.0"
application:
  name: cohort-stats
  type: processor
runtime:
  computeTypes: [standard]
inputs:
  - name: recordings
    mediaTypes: [application/x-edf]
  - name: records
    description: The dataset's subjects and samples.
    conformsTo: pennsieve-records
    placement: subfolder
outputs:
  - name: summary
    mediaTypes: [text/csv]
`

func TestRecordsInputs(t *testing.T) {
	if ps := manifestProblems([]byte(cohortStats)); len(ps) != 0 {
		t.Fatalf("cohort-stats: %v", ps)
	}
	cases := map[string]struct {
		change, with, want string
	}{
		"at the root":        {"    placement: subfolder\noutputs:", "    placement: root\noutputs:", "/inputs/1/placement: an input of metadata records"},
		"placement left out": {"    placement: subfolder\noutputs:", "outputs:", "needs placement: subfolder"},
		"versioned":          {"conformsTo: pennsieve-records\n    placement: subfolder", "conformsTo: pennsieve-records@1.0\n    placement: root", "needs placement: subfolder"},
		"listed first":       {"conformsTo: pennsieve-records\n    placement: subfolder", "conformsTo: [pennsieve-records, https://example.org/cohort]\n    placement: root", "needs placement: subfolder"},
		"blank entry first":  {"conformsTo: pennsieve-records\n    placement: subfolder", "conformsTo: [\"\", \" pennsieve-records \"]\n    placement: \" root \"", "needs placement: subfolder"},
		"an output":          {"mediaTypes: [text/csv]", "conformsTo: pennsieve-records", "/outputs/0/conformsTo: apps can't write metadata records"},
	}
	for name, c := range cases {
		yml := strings.Replace(cohortStats, c.change, c.with, 1)
		if yml == cohortStats {
			t.Fatalf("%s: the change didn't apply", name)
		}
		cfg, err := parseAppConfig([]byte(yml))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ps := viewPartProblems(cfg)
		if len(ps) != 1 || !strings.Contains(ps[0], c.want) {
			t.Errorf("%s: got %v, want one problem with %q", name, ps, c.want)
		}
	}
	// Records nested in another structure, or another structure first, are
	// not a records input.
	for _, ct := range []string{"[bids, pennsieve-records]", "pennsieve-recordsx", "[pennsieve-published-dataset, bids]"} {
		yml := strings.Replace(cohortStats, "conformsTo: pennsieve-records\n    placement: subfolder", "conformsTo: "+ct+"\n    placement: root", 1)
		cfg, err := parseAppConfig([]byte(yml))
		if err != nil {
			t.Fatal(err)
		}
		if ps := viewPartProblems(cfg); ps != nil {
			t.Errorf("conformsTo %s: %v", ct, ps)
		}
	}
}

func TestRecordsProblemsAreAlwaysRefused(t *testing.T) {
	ok, err := parseAppConfig([]byte(cohortStats))
	if err != nil {
		t.Fatal(err)
	}
	if reason := alwaysRefused(ok, "v1.0.0"); reason != "" {
		t.Errorf("cohort-stats refused: %s", reason)
	}
	listed, err := parseAppConfig([]byte(strings.Replace(cohortStats, "conformsTo: pennsieve-records", "conformsTo: [pennsieve-records, https://example.org/cohort]", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if reason := alwaysRefused(listed, "v1.0.0"); reason != "" {
		t.Errorf("records listed first, in its own folder, refused: %s", reason)
	}
	root, err := parseAppConfig([]byte(strings.Replace(cohortStats, "    placement: subfolder\noutputs:", "outputs:", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if reason := alwaysRefused(root, "v1.0.0"); !strings.Contains(reason, "needs placement: subfolder") {
		t.Errorf("records at the root: got %q", reason)
	}
	// What is stored decides: a blank first entry is dropped, so this is a
	// records input at the root, refused in warn mode too.
	t.Setenv("APP_MANIFEST_VALIDATION", "warn")
	blank, err := parseAppConfig([]byte(strings.Replace(cohortStats, "conformsTo: pennsieve-records\n    placement: subfolder", "conformsTo: [\"\", pennsieve-records]\n    placement: root", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if reason := alwaysRefused(blank, "v1.0.0"); !strings.Contains(reason, "/inputs/1/placement") {
		t.Errorf("blank entry before records at the root: got %q", reason)
	}
}

// kgSummary takes a dataset's knowledge graph.
const kgSummary = `schemaVersion: "1.0"
application:
  name: kg-summary
  type: processor
runtime:
  computeTypes: [standard]
inputs:
  - name: graph
    description: The dataset's records as a graph.
    conformsTo: kgx
    placement: subfolder
outputs:
  - name: summary
    mediaTypes: [text/html]
`

// An input of a knowledge graph follows the records' rules: in its own
// folder, and not as an output.
func TestKGXInputs(t *testing.T) {
	if ps := manifestProblems([]byte(kgSummary)); len(ps) != 0 {
		t.Fatalf("kg-summary: %v", ps)
	}
	cases := map[string]struct {
		change, with, want string
	}{
		"at the root":        {"    placement: subfolder\noutputs:", "    placement: root\noutputs:", "/inputs/0/placement: an input of a knowledge graph (conformsTo: kgx) needs placement: subfolder: the graph goes in its own folder"},
		"placement left out": {"    placement: subfolder\noutputs:", "outputs:", "needs placement: subfolder"},
		"versioned":          {"conformsTo: kgx\n    placement: subfolder", "conformsTo: kgx@1.0\n    placement: root", "needs placement: subfolder"},
		"an output":          {"mediaTypes: [text/html]", "conformsTo: kgx", "/outputs/0/conformsTo: apps can't write a knowledge graph (kgx) yet"},
	}
	for name, c := range cases {
		yml := strings.Replace(kgSummary, c.change, c.with, 1)
		if yml == kgSummary {
			t.Fatalf("%s: the change didn't apply", name)
		}
		cfg, err := parseAppConfig([]byte(yml))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ps := viewPartProblems(cfg)
		if len(ps) != 1 || !strings.Contains(ps[0], c.want) {
			t.Errorf("%s: got %v, want one problem with %q", name, ps, c.want)
		}
		if reason := alwaysRefused(cfg, "v1.0.0"); !strings.Contains(reason, c.want) {
			t.Errorf("%s: not refused: %q", name, reason)
		}
	}
	for _, ct := range []string{"[ro-crate, kgx]", "kgxx"} {
		cfg, err := parseAppConfig([]byte(strings.Replace(kgSummary, "conformsTo: kgx\n    placement: subfolder", "conformsTo: "+ct+"\n    placement: root", 1)))
		if err != nil {
			t.Fatal(err)
		}
		if ps := viewPartProblems(cfg); ps != nil {
			t.Errorf("conformsTo %s: %v", ct, ps)
		}
	}
}
