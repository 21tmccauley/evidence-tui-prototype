package platforms

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

func TestJoin_FilesystemAndCatalogAgree_UsesCatalogMetadata(t *testing.T) {
	repo := t.TempDir()
	plats := []Platform{{
		ID:   "aws",
		Path: filepath.Join(repo, "fetchers/aws"),
		Fetchers: []Fetcher{{
			ID:         "aws/iam_policies",
			Name:       "Iam Policies",
			Path:       filepath.Join(repo, "fetchers/aws/iam_policies.sh"),
			PlatformID: "aws",
		}},
	}}
	scripts := []catalog.Script{{
		ID:         "EVD-IAM-POLICIES",
		Name:       "IAM Policies",
		ScriptFile: "fetchers/aws/iam_policies.sh",
		Source:     "aws",
		Key:        "iam_policies",
		Controls:   []string{"AC-01"},
	}}

	got := Join(repo, plats, scripts)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].ID != "EVD-IAM-POLICIES" {
		t.Errorf("expected catalog ID, got %q", got[0].ID)
	}
	if got[0].Name != "IAM Policies" {
		t.Errorf("expected catalog Name, got %q", got[0].Name)
	}
	if len(got[0].Controls) != 1 || got[0].Controls[0] != "AC-01" {
		t.Errorf("controls lost in join: %v", got[0].Controls)
	}
}

func TestJoin_FilesystemHasNoCatalogEntry_Synthesizes(t *testing.T) {
	repo := t.TempDir()
	plats := []Platform{{
		ID: "acme",
		Fetchers: []Fetcher{{
			ID:         "acme/widget_check",
			Name:       "Widget Check",
			Path:       filepath.Join(repo, "fetchers/acme/widget_check.py"),
			PlatformID: "acme",
		}},
	}}

	got := Join(repo, plats, nil)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	s := got[0]
	if s.Source != "acme" || s.Key != "widget_check" {
		t.Errorf("source/key wrong: %+v", s)
	}
	if s.ScriptFile != "fetchers/acme/widget_check.py" {
		t.Errorf("ScriptFile = %q", s.ScriptFile)
	}
	if !regexp.MustCompile(`^EVD-[A-Z0-9]+(-[A-Z0-9]+)+$`).MatchString(s.ID) {
		t.Errorf("synthesized ID %q does not match EVD shape", s.ID)
	}
	if s.ID != "EVD-ACME-WIDGET-CHECK" {
		t.Errorf("ID = %q, want EVD-ACME-WIDGET-CHECK", s.ID)
	}
}

func TestJoin_CatalogScriptMissingFromDisk_StillPreserved(t *testing.T) {
	repo := t.TempDir()
	plats := []Platform{} // nothing on disk
	scripts := []catalog.Script{{
		ID:         "EVD-ORPHAN",
		Name:       "Orphan",
		ScriptFile: "fetchers/aws/orphan.sh",
		Source:     "aws",
		Key:        "orphan",
	}}

	got := Join(repo, plats, scripts)
	if len(got) != 1 || got[0].ID != "EVD-ORPHAN" {
		t.Fatalf("orphan should be preserved, got %+v", got)
	}
}

func TestJoin_SortedByID(t *testing.T) {
	repo := t.TempDir()
	plats := []Platform{{
		ID: "z",
		Fetchers: []Fetcher{
			{ID: "z/b", Path: filepath.Join(repo, "fetchers/z/b.py"), PlatformID: "z", Name: "B"},
			{ID: "z/a", Path: filepath.Join(repo, "fetchers/z/a.py"), PlatformID: "z", Name: "A"},
		},
	}, {
		ID: "a",
		Fetchers: []Fetcher{
			{ID: "a/x", Path: filepath.Join(repo, "fetchers/a/x.py"), PlatformID: "a", Name: "X"},
		},
	}}
	got := Join(repo, plats, nil)
	prev := ""
	for _, s := range got {
		if prev != "" && s.ID < prev {
			t.Fatalf("not sorted: %q before %q", prev, s.ID)
		}
		prev = s.ID
	}
}

func TestSynthesizeID_HandlesEdgeCases(t *testing.T) {
	cases := map[[2]string]string{
		{"aws", "iam_policies"}:        "EVD-AWS-IAM-POLICIES",
		{"my-platform", "foo_bar"}:     "EVD-MY-PLATFORM-FOO-BAR",
		{"with.dots", "x"}:             "EVD-WITHDOTS-X",
		{"", "x"}:                      "EVD-X-X",
		{"x", ""}:                      "EVD-X-X",
		{"aws", "auto__scaling"}:       "EVD-AWS-AUTO-SCALING",
	}
	for in, want := range cases {
		if got := synthesizeID(in[0], in[1]); got != want {
			t.Errorf("synthesizeID(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
