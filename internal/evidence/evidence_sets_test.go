package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

func TestRender_RichTextAndValidationRules(t *testing.T) {
	scripts := map[string]catalog.Script{
		"EVD-AWS-SSL-ENFORCEMENT": {
			ID:           "EVD-AWS-SSL-ENFORCEMENT",
			Key:          "aws_component_ssl_enforcement_status",
			Source:       "aws",
			Name:         "AWS SSL Enforcement",
			Description:  "Evidence for SSL/TLS enforcement",
			ScriptFile:   "fetchers/aws/aws_component_ssl_enforcement_status.sh",
			Instructions: "Script: aws_component_ssl_enforcement_status.sh. Commands executed: aws s3api list-buckets, aws rds describe-db-instances",
			ValidationRules: []catalog.ValidationRule{
				{
					ID:    1,
					Regex: `"s3_total":\s*(?P<s3_total>\d+)&"`,
					Logic: "IF s3_total == s3_ssl_enforced THEN PASS",
				},
				{
					Regex: `"Encrypted":\s*true`,
				},
			},
		},
	}

	doc, err := Render([]string{"EVD-AWS-SSL-ENFORCEMENT"}, scripts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	set := doc.EvidenceSets["aws_component_ssl_enforcement_status"]
	if set.ID != "EVD-AWS-SSL-ENFORCEMENT" {
		t.Fatalf("id: got %q", set.ID)
	}
	if set.Service != "AWS" {
		t.Fatalf("service: got %q want AWS", set.Service)
	}
	if set.ScriptFile != "fetchers/aws/aws_component_ssl_enforcement_status.sh" {
		t.Fatalf("script_file: got %q", set.ScriptFile)
	}
	if len(set.ValidationRules) != 2 {
		t.Fatalf("rules: got %d", len(set.ValidationRules))
	}

	// Regex strings: Python json.dumps shape; SetEscapeHTML(false) for & etc.
	if got, want := set.ValidationRules[0].Regex, `"\"s3_total\":\\s*(?P<s3_total>\\d+)&\""`; got != want {
		t.Fatalf("escaped structured regex:\ngot  %q\nwant %q", got, want)
	}
	if got, want := set.ValidationRules[1].ID, 2; got != want {
		t.Fatalf("bare-string rule id: got %d want %d", got, want)
	}
	if got, want := set.ValidationRules[1].Logic, "IF match.group(1) == expected_value THEN PASS"; got != want {
		t.Fatalf("bare-string rule logic: got %q want %q", got, want)
	}

	raw, err := Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"script_file": "fetchers/aws/aws_component_ssl_enforcement_status.sh"`) {
		t.Fatalf("json missing script_file:\n%s", raw)
	}
	if strings.Contains(string(raw), `\u0026`) {
		t.Fatalf("regex should not HTML-escape &: \n%s", raw)
	}
	if !strings.Contains(string(raw), `"instructions": [`) {
		t.Fatalf("json missing rich-text instructions:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"code": true`) || !strings.Contains(string(raw), "aws s3api list-buckets") {
		t.Fatalf("instructions should include command code spans:\n%s", raw)
	}

	var decoded Document
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal rendered json: %v\n%s", err, raw)
	}
}

func TestRender_SelectionOrderAndMissingIDs(t *testing.T) {
	scripts := map[string]catalog.Script{
		"EVD-ONE": {ID: "EVD-ONE", Key: "one", Source: "aws", Name: "One"},
	}
	if _, err := Render([]string{"EVD-MISSING"}, scripts); err == nil {
		t.Fatal("Render should reject selected IDs that are missing from the catalog")
	}
}

func TestWrite_WritesCompatibilityAndAuditCopies(t *testing.T) {
	dir := t.TempDir()
	compat := filepath.Join(dir, "repo", "evidence_sets.json")
	audit := filepath.Join(dir, "evidence", "2026-05-04T09-00-00Z", "evidence_sets.json")
	scripts := map[string]catalog.Script{
		"EVD-ONE": {
			ID:           "EVD-ONE",
			Key:          "one",
			Source:       "aws",
			Name:         "One",
			Description:  "First",
			ScriptFile:   "fetchers/aws/one.sh",
			Instructions: "Script: one.sh. Commands executed: aws sts get-caller-identity",
		},
	}

	if err := Write([]string{"EVD-ONE"}, scripts, compat, audit); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for _, path := range []string{compat, audit} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(b), `"evidence_sets"`) {
			t.Fatalf("%s missing evidence_sets root:\n%s", path, b)
		}
		if !strings.Contains(string(b), `"one"`) {
			t.Fatalf("%s missing script key:\n%s", path, b)
		}
	}
}
