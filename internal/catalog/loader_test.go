package catalog

import (
	"strings"
	"testing"
)

// The embedded catalog is the source-of-truth contract for the rest of the
// app. If this test fails, every other catalog test is meaningless, so it
// runs first and other tests assume it passed.
func TestEmbeddedParses(t *testing.T) {
	c, scripts, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("embedded catalog must parse: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil Catalog")
	}
	if got := len(scripts); got < 30 {
		t.Errorf("expected at least 30 scripts in embedded catalog, got %d", got)
	}
	if c.Wrapper.Version == "" {
		t.Error("expected non-empty version on embedded catalog")
	}
}

func TestRejectsDuplicateID(t *testing.T) {
	const dup = `{
  "evidence_fetchers_catalog": {
    "version": "0.0.1",
    "categories": {
      "alpha": {
        "name": "Alpha",
        "scripts": {
          "one": {"id": "EVD-DUP-ID", "name": "One"},
          "two": {"id": "EVD-DUP-ID", "name": "Two"}
        }
      }
    }
  }
}`
	_, _, err := Load(strings.NewReader(dup))
	if err == nil {
		t.Fatal("expected duplicate-id error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("expected error to mention duplicate id, got: %v", err)
	}
}

func TestRejectsBadIDShape(t *testing.T) {
	cases := map[string]string{
		"lowercase":          "evd-foo-bar",
		"missing-separator":  "EVD",
		"only-prefix":        "EVD-",
		"single-segment":     "EVD-FOOBAR",
		"mixedcase":          "EVD-Foo-Bar",
		"prefix-typo":        "VED-FOO-BAR",
		"trailing-separator": "EVD-FOO-",
	}
	for name, badID := range cases {
		t.Run(name, func(t *testing.T) {
			body := `{
  "evidence_fetchers_catalog": {
    "version": "0.0.1",
    "categories": {
      "alpha": {
        "name": "Alpha",
        "scripts": {
          "one": {"id": "` + badID + `", "name": "One"}
        }
      }
    }
  }
}`
			_, _, err := Load(strings.NewReader(body))
			if err == nil {
				t.Fatalf("expected invalid-id error for %q, got nil", badID)
			}
			if !strings.Contains(err.Error(), "invalid id") {
				t.Errorf("expected error to mention invalid id, got: %v", err)
			}
		})
	}
}

// .cursor/rules/50-catalog.mdc requires we preserve controls[] and
// solution_capabilities[] on read; this test pins one entry that has both.
func TestPreservesControlsAndCapabilities(t *testing.T) {
	_, scripts, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	got := findByID(scripts, "EVD-AUTO-SCALING-HA")
	if got == nil {
		t.Fatal("EVD-AUTO-SCALING-HA not found in embedded catalog")
	}
	if len(got.Controls) == 0 {
		t.Errorf("expected EVD-AUTO-SCALING-HA to carry at least one control, got 0")
	}
	if len(got.SolutionCapabilities) == 0 {
		t.Errorf("expected EVD-AUTO-SCALING-HA to carry at least one solution capability, got 0")
	}
}

// Source / Key are loader-populated; downstream code (mock adapter,
// evidence-sets renderer, runner) relies on them being filled.
func TestSourceAndKeyPopulated(t *testing.T) {
	_, scripts, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	got := findByID(scripts, "EVD-IAM-POLICIES")
	if got == nil {
		t.Fatal("EVD-IAM-POLICIES not found in embedded catalog")
	}
	if got.Source != "aws" {
		t.Errorf("Source: got %q, want %q", got.Source, "aws")
	}
	if got.Key == "" {
		t.Error("expected non-empty Key (script-map key) on EVD-IAM-POLICIES")
	}
}

func findByID(scripts []Script, id string) *Script {
	for i := range scripts {
		if scripts[i].ID == id {
			return &scripts[i]
		}
	}
	return nil
}
