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
	if err != nil || empty.Version != 1 || empty.Sets == nil || len(empty.Sets) != 0 {
		t.Fatalf("Load on missing file = %+v, %v", empty, err)
	}

	off := false
	data := HighlightRulesData{Sets: []HighlightRuleSet{{
		ID: "olt", Name: "OLT", Global: false,
		Rules: []HighlightRule{
			{ID: "r1", Pattern: "LOS", Kind: "keyword", Color: "#ff0000", WholeWord: true},
			{ID: "r2", Pattern: `-2[7-9]\.\d+`, Kind: "regex", Color: "#ffaa00", Enabled: &off},
		},
	}}}
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
	if got.Version != 1 || len(got.Sets) != 1 || len(got.Sets[0].Rules) != 2 {
		t.Fatalf("round trip = %+v", got)
	}
	if r := got.Sets[0].Rules[1]; r.Enabled == nil || *r.Enabled {
		t.Errorf("enabled=false lost: %+v", r)
	}
}

func TestHighlightRulesValidate(t *testing.T) {
	rule := func(mut func(*HighlightRule)) HighlightRulesData {
		r := HighlightRule{ID: "r", Pattern: "x", Kind: "keyword", Color: "#010203"}
		mut(&r)
		return HighlightRulesData{Sets: []HighlightRuleSet{{ID: "s", Name: "s", Rules: []HighlightRule{r}}}}
	}
	cases := map[string]HighlightRulesData{
		"bad kind":    rule(func(r *HighlightRule) { r.Kind = "glob" }),
		"empty":       rule(func(r *HighlightRule) { r.Pattern = "" }),
		"too long":    rule(func(r *HighlightRule) { r.Pattern = strings.Repeat("a", 501) }),
		"bad color":   rule(func(r *HighlightRule) { r.Color = "red" }),
		"no set id":   {Sets: []HighlightRuleSet{{Name: "x"}}},
		"dup set ids": {Sets: []HighlightRuleSet{{ID: "a"}, {ID: "a"}}},
		"reserved id": {Sets: []HighlightRuleSet{{ID: "preset:x"}}},
		"bad preset":  {PresetGlobals: map[string]bool{"zte": true}},
	}
	for name, d := range cases {
		if err := d.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := (HighlightRulesData{PresetGlobals: map[string]bool{"preset:zte-olt": true}}).Validate(); err != nil {
		t.Errorf("valid preset override rejected: %v", err)
	}
	if err := rule(func(*HighlightRule) {}).Validate(); err != nil {
		t.Errorf("valid data rejected: %v", err)
	}
}
