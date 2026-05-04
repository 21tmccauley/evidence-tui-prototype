package preflight

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAWSProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	body := `[default]
region = us-east-1

[profile staging]
region = us-west-2
sso_session = corp

[profile prod]
role_arn = arn:aws:iam::123456789012:role/Admin
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	profiles, err := LoadAWSProfiles(path)
	if err != nil {
		t.Fatalf("LoadAWSProfiles: %v", err)
	}
	if got, want := len(profiles), 3; got != want {
		t.Fatalf("profiles: got %d want %d: %#v", got, want, profiles)
	}
	if profiles[0].Name != "default" || profiles[0].Region != "us-east-1" {
		t.Fatalf("default profile should sort first with region, got %#v", profiles[0])
	}
	if profiles[1].Name != "prod" || profiles[1].Note != "assume-role profile" {
		t.Fatalf("prod profile note/sort mismatch: %#v", profiles[1])
	}
	if profiles[2].Name != "staging" || profiles[2].Note != "AWS SSO profile" {
		t.Fatalf("staging profile note/sort mismatch: %#v", profiles[2])
	}
}
