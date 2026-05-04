package catalog

import (
	"encoding/json"
	"fmt"
)

var jsonUnmarshal = json.Unmarshal

// Catalog mirrors evidence_fetchers_catalog.json on disk.
type Catalog struct {
	Wrapper Wrapper `json:"evidence_fetchers_catalog"`
}

type Wrapper struct {
	Version     string              `json:"version"`
	Description string              `json:"description"`
	LastUpdated string              `json:"last_updated"`
	Categories  map[string]Category `json:"categories"`
}

type Category struct {
	Name                    string            `json:"name"`
	Description             string            `json:"description"`
	AccessMethod            string            `json:"access_method"`
	AccessMethodDescription string            `json:"access_method_description"`
	Logo                    string            `json:"logo"`
	Scripts                 map[string]Script `json:"scripts"`
}

// Script is one fetcher; Source and Key are set by the loader. Preserve controls and solution_capabilities per .cursor/rules/50-catalog.mdc.
type Script struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	Description          string           `json:"description"`
	ScriptFile           string           `json:"script_file"`
	Instructions         string           `json:"instructions"`
	Dependencies         []string         `json:"dependencies"`
	Tags                 []string         `json:"tags"`
	ValidationRules      []ValidationRule `json:"validation_rules"`
	SolutionCapabilities []string         `json:"solution_capabilities"`
	Controls             []string         `json:"controls"`

	Source string `json:"-"` // category key (e.g., "aws"); set by the loader
	Key    string `json:"-"` // script-map key (e.g., "iam_policies"); set by the loader
}

// ValidationRule is catalog validation_rules JSON: object or bare regex string (see UnmarshalJSON); renderer JSON-escapes for Paramify.
type ValidationRule struct {
	ID    int    `json:"id"`
	Regex string `json:"regex"`
	Logic string `json:"logic"`
}

type validationRuleObject struct {
	ID    int    `json:"id"`
	Regex string `json:"regex"`
	Logic string `json:"logic"`
}

// UnmarshalJSON accepts either the structured `{id, regex, logic}` shape or
// a bare regex string. Other JSON types are an error.
func (v *ValidationRule) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	switch data[0] {
	case '"':
		var s string
		if err := jsonUnmarshal(data, &s); err != nil {
			return err
		}
		v.Regex = s
		return nil
	case '{':
		var obj validationRuleObject
		if err := jsonUnmarshal(data, &obj); err != nil {
			return err
		}
		v.ID = obj.ID
		v.Regex = obj.Regex
		v.Logic = obj.Logic
		return nil
	default:
		return fmt.Errorf("validation_rules entry must be string or object, got %q", string(data))
	}
}
