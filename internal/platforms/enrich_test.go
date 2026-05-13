package platforms

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

func TestEnrichFromRootEnvExample_BucketsByPlatformPrefix(t *testing.T) {
	repo := t.TempDir()
	rootEnv := `
# =============================================================================
# Paramify API Configuration
    PARAMIFY_UPLOAD_API_TOKEN=your_token_here

# Okta Configuration
    # OKTA_API_TOKEN=your_okta_api_token_here
    # OKTA_ORG_URL=https://your-org.okta.com

# AWS Configuration
    # AWS_PROFILE=gov_readonly
    # AWS_DEFAULT_REGION=us-gov-west-1

# Some other section
    # FETCHER_TIMEOUT=300
`
	if err := os.WriteFile(filepath.Join(repo, ".env.example"), []byte(rootEnv), 0o644); err != nil {
		t.Fatal(err)
	}
	plats := []Platform{
		{ID: "aws"},
		{ID: "okta"},
		{ID: "knowbe4"}, // not mentioned in root env
	}

	got := EnrichFromRootEnvExample(plats, repo)

	want := map[string][]string{
		"aws":     {"AWS_PROFILE", "AWS_DEFAULT_REGION"},
		"okta":    {"OKTA_API_TOKEN", "OKTA_ORG_URL"},
		"knowbe4": nil, // no matching keys in this fixture
	}
	for _, p := range got {
		gotNames := envKeyNames(p.EnvKeys)
		wantNames := want[p.ID]
		if len(gotNames) == 0 && len(wantNames) == 0 {
			continue
		}
		sort.Strings(gotNames)
		sort.Strings(wantNames)
		if !reflect.DeepEqual(gotNames, wantNames) {
			t.Errorf("%s keys = %v, want %v", p.ID, gotNames, wantNames)
		}
	}
}

func TestEnrichFromRootEnvExample_PreservesExistingPerPlatformKeys(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".env.example"),
		[]byte("# OKTA_API_TOKEN=from-root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plats := []Platform{{
		ID: "okta",
		EnvKeys: []EnvKey{
			{Name: "OKTA_API_TOKEN", Optional: true}, // already declared per-folder
		},
	}}
	got := EnrichFromRootEnvExample(plats, repo)
	if len(got[0].EnvKeys) != 1 {
		t.Fatalf("per-folder declaration should not be duplicated; got %v", got[0].EnvKeys)
	}
	if !got[0].EnvKeys[0].Optional {
		t.Errorf("per-folder Optional flag should be preserved; got %v", got[0].EnvKeys[0])
	}
}

func TestEnrichFromRootEnvExample_NoFileIsNoop(t *testing.T) {
	repo := t.TempDir() // no .env.example
	plats := []Platform{{ID: "aws"}}
	got := EnrichFromRootEnvExample(plats, repo)
	if len(got[0].EnvKeys) != 0 {
		t.Fatalf("no env file should yield no keys, got %v", got[0].EnvKeys)
	}
}

func TestEnrichFromRootEnvExample_LongestPrefixWins(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".env.example"),
		[]byte("# AWS_GOVCLOUD_REGION=us-gov-west-1\n# AWS_PROFILE=default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plats := []Platform{
		{ID: "aws"},
		{ID: "aws_govcloud"},
	}
	got := EnrichFromRootEnvExample(plats, repo)
	keysByID := map[string][]string{}
	for _, p := range got {
		keysByID[p.ID] = envKeyNames(p.EnvKeys)
	}
	if got, want := keysByID["aws_govcloud"], []string{"AWS_GOVCLOUD_REGION"}; !reflect.DeepEqual(got, want) {
		t.Errorf("aws_govcloud should claim AWS_GOVCLOUD_REGION, got %v", got)
	}
	if got, want := keysByID["aws"], []string{"AWS_PROFILE"}; !reflect.DeepEqual(got, want) {
		t.Errorf("aws should only get AWS_PROFILE, got %v", got)
	}
}

func TestEnrichFromCatalog_UsesCategoryName(t *testing.T) {
	plats := []Platform{
		{ID: "aws", DisplayName: "Aws"},
		{ID: "okta", DisplayName: "Okta"},
		{ID: "unknown", DisplayName: "Unknown"},
	}
	cat := &catalog.Catalog{
		Wrapper: catalog.Wrapper{
			Categories: map[string]catalog.Category{
				"aws":  {Name: "Amazon Web Services"},
				"okta": {Name: "Okta"},
			},
		},
	}
	got := EnrichFromCatalog(plats, cat)
	names := map[string]string{}
	for _, p := range got {
		names[p.ID] = p.DisplayName
	}
	if names["aws"] != "Amazon Web Services" {
		t.Errorf("aws DisplayName = %q, want 'Amazon Web Services'", names["aws"])
	}
	if names["unknown"] != "Unknown" {
		t.Errorf("unknown DisplayName should be unchanged, got %q", names["unknown"])
	}
}

func TestExtractDeclaredKeys_HandlesCommentsAndIndentation(t *testing.T) {
	body := `
# comment with = sign, not a key
   # FOO=value

BAR=
    # BAZ=indented commented
QUX = whitespace around eq
`
	got := extractDeclaredKeys(body)
	want := []string{"FOO", "BAR", "BAZ", "QUX"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

