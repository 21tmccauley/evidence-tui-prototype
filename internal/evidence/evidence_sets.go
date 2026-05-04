package evidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

// Document is evidence_sets.json on disk (Paramify uploader shape).
type Document struct {
	EvidenceSets map[string]Set `json:"evidence_sets"`
}

type Set struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Service         string           `json:"service"`
	Instructions    []RichNode       `json:"instructions"`
	ValidationRules []ValidationRule `json:"validationRules"`
	ScriptFile      string           `json:"script_file"`
}

// ValidationRule uses JSON-escaped regex strings (Python json.dumps shape), not raw catalog regex.
type ValidationRule struct {
	ID    int    `json:"id"`
	Regex string `json:"regex"`
	Logic string `json:"logic"`
}

type RichNode struct {
	Type     string     `json:"type,omitempty"`
	Children []RichNode `json:"children,omitempty"`
	Bold     bool       `json:"bold,omitempty"`
	Code     bool       `json:"code,omitempty"`
	Text     string     `json:"text,omitempty"`
}

var (
	scriptPattern   = regexp.MustCompile(`Script:\s*([^\s.]+)`)
	commandsPattern = regexp.MustCompile(`Commands executed:\s*(.+)`)
)

// Render builds evidence_sets.json keyed by script key, in selection order.
func Render(selectedIDs []string, scripts map[string]catalog.Script) (Document, error) {
	doc := Document{EvidenceSets: map[string]Set{}}
	for _, id := range selectedIDs {
		s, ok := scripts[id]
		if !ok {
			return Document{}, fmt.Errorf("selected fetcher %s not found in catalog", id)
		}
		if s.Key == "" {
			return Document{}, fmt.Errorf("selected fetcher %s has empty script key", id)
		}
		if _, exists := doc.EvidenceSets[s.Key]; exists {
			return Document{}, fmt.Errorf("duplicate evidence set key %q", s.Key)
		}

		rules, err := processValidationRules(s.ValidationRules)
		if err != nil {
			return Document{}, fmt.Errorf("process validation rules for %s: %w", s.Key, err)
		}
		doc.EvidenceSets[s.Key] = Set{
			ID:              s.ID,
			Name:            s.Name,
			Description:     s.Description,
			Service:         strings.ToUpper(s.Source),
			Instructions:    convertInstructionsToRichText(s.Instructions, rules),
			ValidationRules: rules,
			ScriptFile:      s.ScriptFile,
		}
	}
	return doc, nil
}

// Write renders evidence_sets.json to each non-empty path (mkdir parents).
func Write(selectedIDs []string, scripts map[string]catalog.Script, paths ...string) error {
	doc, err := Render(selectedIDs, scripts)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no evidence_sets.json output paths supplied")
	}
	payload, err := Marshal(doc)
	if err != nil {
		return err
	}
	wrote := false
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create parent for %q: %w", path, err)
		}
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			return fmt.Errorf("write %q: %w", path, err)
		}
		wrote = true
	}
	if !wrote {
		return fmt.Errorf("no evidence_sets.json output paths supplied")
	}
	return nil
}

// Marshal is indented JSON with SetEscapeHTML(false) for Python-compatible regex encoding.
func Marshal(doc Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func processValidationRules(rules []catalog.ValidationRule) ([]ValidationRule, error) {
	out := make([]ValidationRule, 0, len(rules))
	for i, rule := range rules {
		id := rule.ID
		logic := rule.Logic
		if id == 0 {
			id = i + 1
		}
		if logic == "" {
			logic = "IF match.group(1) == expected_value THEN PASS"
		}
		escaped, err := escapeRegexForJSON(rule.Regex)
		if err != nil {
			return nil, err
		}
		out = append(out, ValidationRule{
			ID:    id,
			Regex: escaped,
			Logic: logic,
		})
	}
	return out, nil
}

func escapeRegexForJSON(pattern string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pattern); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func convertInstructionsToRichText(instructions string, rules []ValidationRule) []RichNode {
	scriptName, commands := parseInstructions(instructions)
	return createRichTextInstructions(scriptName, commands, rules)
}

func parseInstructions(instructions string) (string, []string) {
	scriptName := "unknown_script.sh"
	if match := scriptPattern.FindStringSubmatch(instructions); len(match) == 2 {
		scriptName = match[1]
	}
	commands := []string{}
	if match := commandsPattern.FindStringSubmatch(instructions); len(match) == 2 {
		for _, part := range strings.Split(match[1], ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				commands = append(commands, part)
			}
		}
	}
	return scriptName, commands
}

func createRichTextInstructions(scriptName string, commands []string, rules []ValidationRule) []RichNode {
	nodes := []RichNode{
		paragraph(spanBold("Script:"), spanText(" "), spanCode(scriptName)),
		paragraph(spanText("")),
		paragraph(spanBold("Commands: ")),
		unorderedList(codeListItems(commands)...),
		paragraph(spanText("")),
	}
	if len(rules) == 0 {
		return nodes
	}
	nodes = append(nodes, paragraph(spanBold("Validation:")))
	for _, rule := range rules {
		nodes = append(nodes, paragraph(spanText(fmt.Sprintf("Rule %d", rule.ID))))
		details := []RichNode{}
		if rule.Regex != "" {
			details = append(details, listItem(spanText("Regex: "), spanCode(rule.Regex)))
		}
		if rule.Logic != "" {
			details = append(details, listItem(spanText("Logic: "), spanCode(rule.Logic)))
		}
		if len(details) > 0 {
			nodes = append(nodes, unorderedList(details...))
		}
	}
	return nodes
}

func codeListItems(values []string) []RichNode {
	out := make([]RichNode, 0, len(values))
	for _, value := range values {
		out = append(out, listItem(spanCode(value)))
	}
	return out
}

func paragraph(children ...RichNode) RichNode {
	return RichNode{Type: "p", Children: children}
}

func unorderedList(children ...RichNode) RichNode {
	return RichNode{Type: "ul", Children: children}
}

func listItem(children ...RichNode) RichNode {
	return RichNode{
		Type: "li",
		Children: []RichNode{{
			Type:     "lic",
			Children: children,
		}},
	}
}

func spanBold(text string) RichNode {
	return RichNode{Bold: true, Text: text}
}

func spanCode(text string) RichNode {
	return RichNode{Code: true, Text: text}
}

func spanText(text string) RichNode {
	return RichNode{Text: text}
}
