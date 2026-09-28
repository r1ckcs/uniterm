package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const highlightRulesFileName = "highlightRules.json"

// HighlightRule is one user keyword/regex highlight rule. Matching happens in
// the frontend (overlayHighlight.ts), which also validates regexes for
// runtime cost; the backend only enforces shape and size so a synced or
// hand-edited file cannot carry garbage into every terminal.
type HighlightRule struct {
	ID            string `json:"id"`
	Pattern       string `json:"pattern"`
	Kind          string `json:"kind"` // "keyword" | "regex"
	Color         string `json:"color"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
	WholeWord     bool   `json:"wholeWord,omitempty"`
	// Enabled is a pointer so a missing field (older files) means enabled.
	Enabled *bool `json:"enabled,omitempty"`
}

// HighlightRuleSet groups rules; Global sets apply to every terminal, others
// only to connections that list the set id in highlightSets.
type HighlightRuleSet struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Global bool            `json:"global"`
	Rules  []HighlightRule `json:"rules"`
}

type HighlightRulesData struct {
	Version int                `json:"version"`
	Sets    []HighlightRuleSet `json:"sets"`
	// PresetGlobals overrides whether a bundled preset (id "preset:…") is
	// global; presets without an entry use their built-in default.
	PresetGlobals map[string]bool `json:"presetGlobals,omitempty"`
}

const (
	maxHighlightSets         = 200
	maxHighlightRulesPerSet  = 500
	maxHighlightPatternBytes = 500
)

var highlightColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate enforces the shape invariants the frontend relies on.
func (d HighlightRulesData) Validate() error {
	if len(d.Sets) > maxHighlightSets {
		return fmt.Errorf("too many highlight rule sets (%d > %d)", len(d.Sets), maxHighlightSets)
	}
	if len(d.PresetGlobals) > maxHighlightSets {
		return fmt.Errorf("too many preset entries")
	}
	for id := range d.PresetGlobals {
		if !strings.HasPrefix(id, "preset:") || len(id) > 64 {
			return fmt.Errorf("invalid preset id %q", id)
		}
	}
	seen := map[string]bool{}
	for _, s := range d.Sets {
		if strings.HasPrefix(s.ID, "preset:") {
			return fmt.Errorf("set id %q uses the reserved preset prefix", s.ID)
		}
		if s.ID == "" {
			return fmt.Errorf("highlight rule set without id")
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate highlight rule set id %q", s.ID)
		}
		seen[s.ID] = true
		if len(s.Rules) > maxHighlightRulesPerSet {
			return fmt.Errorf("set %q has too many rules", s.Name)
		}
		for _, r := range s.Rules {
			if r.Kind != "keyword" && r.Kind != "regex" {
				return fmt.Errorf("rule %q: invalid kind %q", r.ID, r.Kind)
			}
			if r.Pattern == "" || len(r.Pattern) > maxHighlightPatternBytes {
				return fmt.Errorf("rule %q: pattern must be 1-%d bytes", r.ID, maxHighlightPatternBytes)
			}
			if !highlightColorRe.MatchString(r.Color) {
				return fmt.Errorf("rule %q: invalid color %q", r.ID, r.Color)
			}
		}
	}
	return nil
}

type HighlightRulesStore struct {
	configDir string
}

func NewHighlightRulesStore(configDir string) *HighlightRulesStore {
	return &HighlightRulesStore{configDir: configDir}
}

func (s *HighlightRulesStore) filePath() string {
	return filepath.Join(s.configDir, highlightRulesFileName)
}

func (s *HighlightRulesStore) Save(data HighlightRulesData) error {
	if err := data.Validate(); err != nil {
		return err
	}
	if data.Version == 0 {
		data.Version = 1
	}
	if data.Sets == nil {
		data.Sets = []HighlightRuleSet{}
	}
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.filePath(), bytes, 0600)
}

func (s *HighlightRulesStore) Load() (HighlightRulesData, error) {
	bytes, err := os.ReadFile(s.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return HighlightRulesData{Version: 1, Sets: []HighlightRuleSet{}}, nil
		}
		return HighlightRulesData{}, err
	}
	var data HighlightRulesData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return HighlightRulesData{}, err
	}
	if data.Version == 0 {
		data.Version = 1
	}
	if data.Sets == nil {
		data.Sets = []HighlightRuleSet{}
	}
	return data, nil
}
