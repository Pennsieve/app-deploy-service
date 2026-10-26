package handler

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReadmeSummary(t *testing.T) {
	cases := []struct{ name, md, want string }{
		{
			"the first paragraph, skipping the title and badges",
			"# pennsieve-jupyter-session\n\n[![build](https://x/badge.svg)](https://x)\n\nAn **interactive** Pennsieve workflow processor: it runs a live\nJupyterLab kernel as a [step](https://docs) in a workflow.\n\nSecond paragraph.",
			"An interactive Pennsieve workflow processor: it runs a live JupyterLab kernel as a step in a workflow.",
		},
		{"HTML and code are left out", "<p align=\"center\"><img src=\"logo.png\"></p>\n\n```bash\nmake\n```\n\nConverts `EDF` to NWB.", "Converts EDF to NWB."},
		{"no prose at all", "# Title\n\n![logo](logo.png)\n", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := readmeSummary(c.md); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestReadmeSummary_CutAtAWord(t *testing.T) {
	long := strings.Repeat("analysis ", 60)
	got := readmeSummary(long)
	if !strings.HasSuffix(got, "…") || utf8.RuneCountInString(got) > summaryMaxChars+1 || strings.Contains(got, "analys…") {
		t.Fatalf("got %q (%d)", got, utf8.RuneCountInString(got))
	}
}
