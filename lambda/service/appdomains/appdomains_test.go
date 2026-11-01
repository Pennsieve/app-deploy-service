package appdomains

import (
	"regexp"
	"strings"
	"testing"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

func normalize(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// Every spelling names one domain, and one field: a tag that named two domains
// would colour a card by whichever came first.
func TestTable(t *testing.T) {
	tab, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if tab.ID != URL {
		t.Fatalf("$id %q, want %q", tab.ID, URL)
	}
	if len(tab.Domains) == 0 || len(tab.Fields) == 0 {
		t.Fatal("no domains or fields")
	}
	byTag := map[string]string{}
	for _, d := range tab.Domains {
		if d.Key == "" || d.Label == "" || d.Tag == "" || d.Icon == "" || d.Hue < 0 || d.Hue >= 360 {
			t.Errorf("domain %q is incomplete", d.Key)
		}
		for _, s := range append([]string{d.Tag, d.Key, d.Label}, d.Aliases...) {
			n := normalize(s)
			if other, ok := byTag[n]; ok && other != d.Key {
				t.Errorf("%q names both %s and %s", s, other, d.Key)
			}
			byTag[n] = d.Key
		}
	}
	fieldByTag := map[string]string{}
	for _, f := range tab.Fields {
		for _, s := range append([]string{f.Tag, f.Key, f.Label}, f.Aliases...) {
			n := normalize(s)
			if other, ok := fieldByTag[n]; ok && other != f.Key {
				t.Errorf("%q names both fields %s and %s", s, other, f.Key)
			}
			fieldByTag[n] = f.Key
		}
	}
	if tab.Colors.Neutral.Accent != nil || tab.Colors.Subject.Accent == nil || tab.Colors.Task.Accent == nil {
		t.Error("a neutral card has no accent; subjects and tasks do")
	}
}
