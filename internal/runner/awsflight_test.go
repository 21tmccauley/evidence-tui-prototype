package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAuthChecker lets tests assert the runner consulted the pre-flight
// without shelling out to a real `aws` CLI.
type fakeAuthChecker struct {
	calls int
	err   error
}

func (f *fakeAuthChecker) CheckAWSAuth(_ context.Context, _ string, _ string) error {
	f.calls++
	return f.err
}

func TestValidateAWSEvidence_Known(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kms_key_rotation.json"), `{
  "metadata": {"account_id": "123456789012", "arn": "arn:aws:iam::123:role/x"}
}`)
	if err := ValidateAWSEvidence(dir, "kms_key_rotation"); err != nil {
		t.Errorf("known identity should pass, got: %v", err)
	}
}

func TestValidateAWSEvidence_Unknown(t *testing.T) {
	cases := []string{
		`{"metadata":{"account_id":"unknown","arn":"arn:aws:iam::123:role/x"}}`,
		`{"metadata":{"account_id":"123","arn":"unknown"}}`,
		`{"metadata":{"account_id":"unknown","arn":"unknown"}}`,
	}
	for i, body := range cases {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "iam_policies.json"), body)
		err := ValidateAWSEvidence(dir, "iam_policies")
		if err == nil {
			t.Errorf("case %d: expected unknown-identity error, got nil", i)
			continue
		}
		if !strings.Contains(err.Error(), "unknown AWS identity") {
			t.Errorf("case %d: error should mention unknown AWS identity, got: %v", i, err)
		}
	}
}

// Missing file or unparseable JSON: don't guess. Mirrors Python behavior.
func TestValidateAWSEvidence_MissingFile(t *testing.T) {
	if err := ValidateAWSEvidence(t.TempDir(), "doesnt_exist"); err != nil {
		t.Errorf("missing file should pass through, got: %v", err)
	}
}

func TestValidateAWSEvidence_Malformed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.json"), `not json at all`)
	if err := ValidateAWSEvidence(dir, "bad"); err != nil {
		t.Errorf("malformed JSON should pass through, got: %v", err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
