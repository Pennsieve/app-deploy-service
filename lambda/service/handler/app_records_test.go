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
		ps := recordsProblems(cfg)
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
		if ps := recordsProblems(cfg); ps != nil {
			t.Errorf("conformsTo %s: %v", ct, ps)
		}
	}
}
