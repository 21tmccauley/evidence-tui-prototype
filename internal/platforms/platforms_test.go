package platforms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscover_EmptyRepoReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no platforms, got %d", len(got))
	}
}

func TestDiscover_RejectsEmptyRoot(t *testing.T) {
	if _, err := Discover(""); err == nil {
		t.Fatal("expected error for empty repoRoot")
	}
}

func TestDiscover_SinglePlatformNoManifest(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/aws/iam_policies.py":         "print('hi')\n",
		"fetchers/aws/s3_encryption_status.sh": "echo hi\n",
		"fetchers/aws/_helper.py":              "ignored\n",
		"fetchers/aws/notes.md":                "ignored\n",
		"fetchers/aws/.hidden.py":              "ignored\n",
	})

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 platform, got %d", len(got))
	}
	aws := got[0]
	if aws.ID != "aws" {
		t.Fatalf("ID = %q, want aws", aws.ID)
	}
	if aws.DisplayName != "Aws" {
		t.Fatalf("DisplayName = %q, want humanized 'Aws'", aws.DisplayName)
	}
	if len(aws.EnvKeys) != 0 {
		t.Fatalf("expected no env keys, got %v", aws.EnvKeys)
	}
	if len(aws.Fetchers) != 2 {
		t.Fatalf("want 2 fetchers, got %d (%v)", len(aws.Fetchers), aws.Fetchers)
	}
	if aws.Fetchers[0].ID != "aws/iam_policies" || aws.Fetchers[1].ID != "aws/s3_encryption_status" {
		t.Fatalf("fetcher IDs: %+v", aws.Fetchers)
	}
	if aws.Fetchers[0].Name != "Iam Policies" {
		t.Fatalf("Name = %q, want humanized", aws.Fetchers[0].Name)
	}
}

func TestDiscover_PlatformJSONOverridesDisplayName(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/m365/users.py":   "x\n",
		"fetchers/m365/platform.json": mustJSON(t, Manifest{DisplayName: "Microsoft 365"}),
	})
	got, _ := Discover(dir)
	if got[0].DisplayName != "Microsoft 365" {
		t.Fatalf("DisplayName = %q, want 'Microsoft 365'", got[0].DisplayName)
	}
}

func TestDiscover_EnvExampleParsedAndOrdered(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/okta/users.py": "x\n",
		"fetchers/okta/.env.example": `
# Okta credentials
OKTA_API_TOKEN=
OKTA_ORG_URL=https://example.okta.com

# duplicate (should be ignored on second occurrence)
OKTA_API_TOKEN=again

= bad-line-no-key
`,
	})
	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	names := envKeyNames(got[0].EnvKeys)
	want := []string{"OKTA_API_TOKEN", "OKTA_ORG_URL"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("env keys = %v, want %v", names, want)
	}
}

func TestDiscover_EnvExampleWinsOverManifestKeys(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/gitlab/projects.py": "x\n",
		"fetchers/gitlab/platform.json": mustJSON(t, Manifest{
			EnvKeys: []EnvKey{{Name: "FROM_MANIFEST"}},
		}),
		"fetchers/gitlab/.env.example": "FROM_ENV_EXAMPLE=\n",
	})
	got, _ := Discover(dir)
	if got[0].EnvKeys[0].Name != "FROM_ENV_EXAMPLE" {
		t.Fatalf(".env.example should win; got %v", got[0].EnvKeys)
	}
}

func TestDiscover_ManifestEnvKeysWhenNoExample(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/datadog/runs.py": "x\n",
		"fetchers/datadog/platform.json": mustJSON(t, Manifest{
			EnvKeys: []EnvKey{
				{Name: "DD_API_KEY"},
				{Name: "DD_APP_KEY", Optional: true},
			},
		}),
	})
	got, _ := Discover(dir)
	if len(got[0].EnvKeys) != 2 {
		t.Fatalf("want 2 keys, got %v", got[0].EnvKeys)
	}
	if !got[0].EnvKeys[1].Optional {
		t.Fatalf("DD_APP_KEY should be optional")
	}
}

func TestEnvKey_UnmarshalAcceptsBareString(t *testing.T) {
	var got []EnvKey
	if err := json.Unmarshal([]byte(`["FOO", {"name":"BAR","optional":true}]`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got[0].Name != "FOO" || got[0].Optional {
		t.Fatalf("FOO parsed wrong: %+v", got[0])
	}
	if got[1].Name != "BAR" || !got[1].Optional {
		t.Fatalf("BAR parsed wrong: %+v", got[1])
	}
}

func TestDiscover_SkipsHiddenAndUnderscorePlatforms(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/aws/x.py":          "x\n",
		"fetchers/.hidden/x.py":      "x\n",
		"fetchers/_internal/x.py":    "x\n",
	})
	got, _ := Discover(dir)
	if len(got) != 1 || got[0].ID != "aws" {
		t.Fatalf("want only aws, got %+v", got)
	}
}

func TestDiscover_PlatformsSortedByID(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/zeta/a.py":  "x\n",
		"fetchers/alpha/a.py": "x\n",
		"fetchers/mid/a.py":   "x\n",
	})
	got, _ := Discover(dir)
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	want := []string{"alpha", "mid", "zeta"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
}

func TestDiscover_ManifestUnknownFieldsIgnored(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/future/x.py":         "x\n",
		"fetchers/future/platform.json": `{"display_name":"Future","auth":{"type":"oauth_device"},"version":2}`,
	})
	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover should ignore unknown fields, got: %v", err)
	}
	if got[0].DisplayName != "Future" {
		t.Fatalf("DisplayName = %q", got[0].DisplayName)
	}
}

func TestDiscover_MalformedManifestErrors(t *testing.T) {
	dir := makeRepo(t, map[string]string{
		"fetchers/broken/x.py":         "x\n",
		"fetchers/broken/platform.json": `{not json`,
	})
	if _, err := Discover(dir); err == nil {
		t.Fatal("expected error for malformed platform.json")
	}
}

// --- helpers ---

func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return dir
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func envKeyNames(keys []EnvKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Name
	}
	return out
}
