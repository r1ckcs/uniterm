package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHighlightRulesStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewHighlightRulesStore(dir)

	empty, err := s.Load()
	if err != nil || empty.Rules != nil {
		t.Fatalf("Load on missing file = %+v, %v (want nil rules so the UI seeds defaults)", empty, err)
	}

	off := false
	data := HighlightRulesData{Rules: []HighlightRule{
		{ID: "r1", Pattern: "LOS", Kind: "keyword", Color: "#ff0000", WholeWord: true, Group: "OLT ZTE"},
		{ID: "r2", Pattern: `-2[7-9]\.\d+`, Kind: "regex", Color: "#ffaa00", Enabled: &off},
		{ID: "r3", Pattern: `(^|\s)(/\S+)`, Kind: "regex", Color: "theme:magenta", TrimLead: true},
		{ID: "r4", Pattern: `(¨( *)?#[^¨]+)+`, Kind: "regex", Color: "theme:green", LineMarker: "¨", ColorGroup1: true},
	}}
	if err := s.Save(data); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, highlightRulesFileName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %v, %v", fi.Mode().Perm(), err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || len(got.Rules) != 4 || got.Rules[0].Group != "OLT ZTE" || !got.Rules[2].TrimLead ||
		got.Rules[3].LineMarker != "¨" || !got.Rules[3].ColorGroup1 {
		t.Fatalf("round trip = %+v", got)
	}
	if r := got.Rules[1]; r.Enabled == nil || *r.Enabled {
		t.Errorf("enabled=false lost: %+v", r)
	}
}

func TestHighlightRulesLegacyFileLoads(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"sets":[{"id":"s","name":"Old","global":true,"rules":[{"id":"a","pattern":"x","kind":"keyword","color":"#010203"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, highlightRulesFileName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewHighlightRulesStore(dir).Load()
	if err != nil || got.Version != 1 || got.Rules != nil || len(got.Sets) != 1 {
		t.Fatalf("legacy load = %+v, %v", got, err)
	}
}

func TestHighlightRulesValidate(t *testing.T) {
	rule := func(mut func(*HighlightRule)) HighlightRulesData {
		r := HighlightRule{ID: "r", Pattern: "x", Kind: "keyword", Color: "#010203"}
		mut(&r)
		return HighlightRulesData{Rules: []HighlightRule{r}}
	}
	cases := map[string]HighlightRulesData{
		"bad kind":   rule(func(r *HighlightRule) { r.Kind = "glob" }),
		"empty":      rule(func(r *HighlightRule) { r.Pattern = "" }),
		"too long":   rule(func(r *HighlightRule) { r.Pattern = strings.Repeat("a", 2001) }),
		"bad color":  rule(func(r *HighlightRule) { r.Color = "red" }),
		"bad theme":  rule(func(r *HighlightRule) { r.Color = "theme:" }),
		"long group": rule(func(r *HighlightRule) { r.Group = strings.Repeat("g", 61) }),
		"too many":   {Rules: make([]HighlightRule, 2001)},
	}
	for name, d := range cases {
		if err := d.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	for _, c := range []string{"#010203", "theme:red", "theme:brightMagenta"} {
		if err := rule(func(r *HighlightRule) { r.Color = c }).Validate(); err != nil {
			t.Errorf("color %q rejected: %v", c, err)
		}
	}
}
