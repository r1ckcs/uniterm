package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// TrimLead: regex group 1 is a consumed left guard, not colored.
	TrimLead bool `json:"trimLead,omitempty"`
	// LineMarker: MobaXterm-style rule matched against marker+line+marker
	// (the marker, e.g. "¨", stands for line start/end in the pattern).
	LineMarker string `json:"lineMarker,omitempty"`
	// ColorGroup1: color capture group 1 instead of the whole match.
	ColorGroup1 bool `json:"colorGroup1,omitempty"`
	// Group is a free-text label that organizes the list.
	Group string `json:"group,omitempty"`
	// Enabled is a pointer so a missing field (older files) means enabled.
	Enabled *bool `json:"enabled,omitempty"`
}

// HighlightRuleSet is the version-1 layout (named sets, global or per
// connection). It is only read so the frontend can migrate old files.
type HighlightRuleSet struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Global bool            `json:"global"`
	Rules  []HighlightRule `json:"rules"`
}

// HighlightRulesData is highlightRules.json: one ordered rule list applied
// to every terminal (earlier rules win overlaps). Rules is nil until the
// frontend seeds the defaults on first run.
type HighlightRulesData struct {
	Version int             `json:"version"`
	Rules   []HighlightRule `json:"rules"`
	// Sets is the legacy (version 1) layout, kept for migration only.
	Sets []HighlightRuleSet `json:"sets,omitempty"`
}

const (
	maxHighlightRules        = 2000
	maxHighlightSets         = 200
	maxHighlightRulesPerSet  = 500
	maxHighlightPatternBytes = 2000
	maxHighlightGroupBytes   = 60
)

// "#rrggbb", or "theme:<palette key>" to follow the terminal theme.
var highlightColorRe = regexp.MustCompile(`^(?:#[0-9a-fA-F]{6}|theme:[A-Za-z]{2,20})$`)

func (r HighlightRule) validate() error {
	if r.Kind != "keyword" && r.Kind != "regex" {
		return fmt.Errorf("rule %q: invalid kind %q", r.ID, r.Kind)
	}
	if r.Pattern == "" || len(r.Pattern) > maxHighlightPatternBytes {
		return fmt.Errorf("rule %q: pattern must be 1-%d bytes", r.ID, maxHighlightPatternBytes)
	}
	if !highlightColorRe.MatchString(r.Color) {
		return fmt.Errorf("rule %q: invalid color %q", r.ID, r.Color)
	}
	if len(r.LineMarker) > 8 {
		return fmt.Errorf("rule %q: line marker too long", r.ID)
	}
	if len(r.Group) > maxHighlightGroupBytes {
		return fmt.Errorf("rule %q: group label too long", r.ID)
	}
	return nil
}

// Validate enforces the shape invariants the frontend relies on.
func (d HighlightRulesData) Validate() error {
	if len(d.Rules) > maxHighlightRules {
		return fmt.Errorf("too many highlight rules (%d > %d)", len(d.Rules), maxHighlightRules)
	}
	for _, r := range d.Rules {
		if err := r.validate(); err != nil {
			return err
		}
	}
	if len(d.Sets) > maxHighlightSets {
		return fmt.Errorf("too many legacy highlight rule sets")
	}
	for _, s := range d.Sets {
		if len(s.Rules) > maxHighlightRulesPerSet {
			return fmt.Errorf("set %q has too many rules", s.Name)
		}
		for _, r := range s.Rules {
			if err := r.validate(); err != nil {
				return err
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
		data.Version = 2
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
			// Rules stays nil: the frontend seeds the default list.
			return HighlightRulesData{}, nil
		}
		return HighlightRulesData{}, err
	}
	var data HighlightRulesData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return HighlightRulesData{}, err
	}
	return data, nil
}
