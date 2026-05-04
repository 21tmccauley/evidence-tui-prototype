package runner

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

func TestBuildCmd_BashScript(t *testing.T) {
	cfg := Config{
		Profile:         "paramify-prod",
		Region:          "us-east-1",
		FetcherRepoRoot: "/repo",
		OutputRoot:      "/repo/evidence/2026-05-01",
	}
	cmd := BuildCmd(context.Background(), cfg, catalog.Script{
		ScriptFile: "fetchers/aws/iam_policies.sh",
		Key:        "iam_policies",
	})

	if got, want := filepath.Base(cmd.Path), "bash"; got != want {
		t.Errorf("interpreter: got %q, want %q", got, want)
	}
	wantArgs := []string{
		"bash",
		"/repo/fetchers/aws/iam_policies.sh",
		"paramify-prod", "us-east-1", "/repo/evidence/2026-05-01/iam_policies", "/dev/null",
		"--profile", "paramify-prod",
		"--region", "us-east-1",
		"--output-dir", "/repo/evidence/2026-05-01/iam_policies",
	}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Errorf("args mismatch:\n got %#v\nwant %#v", cmd.Args, wantArgs)
	}
	if cmd.Dir != "/repo" {
		t.Errorf("Dir: got %q, want %q", cmd.Dir, "/repo")
	}
	if !slices.Contains(cmd.Env, "EVIDENCE_DIR=/repo/evidence/2026-05-01/iam_policies") {
		t.Errorf("Env missing per-fetcher EVIDENCE_DIR: %v", cmd.Env)
	}
}

func TestBuildCmd_PythonScript(t *testing.T) {
	cfg := Config{FetcherRepoRoot: "/repo"}
	cmd := BuildCmd(context.Background(), cfg, catalog.Script{
		ScriptFile: "fetchers/ssllabs/ssllabs_tls_scan.py",
	})
	if !strings.Contains(filepath.Base(cmd.Path), "python3") {
		t.Errorf(".py script must run via python3, got Path=%q", cmd.Path)
	}
}

func TestBuildCmd_OmitsEmptyFlags(t *testing.T) {
	cfg := Config{
		FetcherRepoRoot: "/repo",
		OutputRoot:      "/repo/evidence/x",
	}
	cmd := BuildCmd(context.Background(), cfg, catalog.Script{
		ScriptFile: "fetchers/aws/foo.sh",
	})
	if slices.Contains(cmd.Args, "--profile") {
		t.Errorf("--profile must be omitted when Profile is empty: %v", cmd.Args)
	}
	if slices.Contains(cmd.Args, "--region") {
		t.Errorf("--region must be omitted when Region is empty: %v", cmd.Args)
	}
	// --output-dir is always set.
	if !slices.Contains(cmd.Args, "--output-dir") {
		t.Errorf("--output-dir must always be present: %v", cmd.Args)
	}
	// 4 positional args + interpreter + script + (--output-dir, value) = 8.
	if got, want := len(cmd.Args), 8; got != want {
		t.Errorf("argv length: got %d, want %d (%v)", got, want, cmd.Args)
	}
}

func TestBuildInstanceCmd_UsesInstanceEnvProfileRegionAndOutputDir(t *testing.T) {
	cfg := Config{
		Profile:         "base-profile",
		Region:          "us-west-2",
		FetcherRepoRoot: "/repo",
		OutputRoot:      "/repo/evidence/2026-05-01",
		Environ:         []string{"BASE=1"},
	}
	inst := Instance{
		ID:     "EVD-S3-ENC_region_1",
		BaseID: "EVD-S3-ENC",
		Name:   "region_1",
		Env: map[string]string{
			"AWS_PROFILE":        "instance-profile",
			"AWS_DEFAULT_REGION": "us-east-1",
			"EXTRA":              "yes",
		},
	}
	cmd := BuildInstanceCmd(context.Background(), cfg, catalog.Script{
		ID:         "EVD-S3-ENC",
		ScriptFile: "fetchers/aws/s3_encryption_status.sh",
		Key:        "s3_encryption_status",
	}, inst)

	wantOut := "/repo/evidence/2026-05-01/EVD-S3-ENC_region_1"
	wantArgs := []string{
		"bash",
		"/repo/fetchers/aws/s3_encryption_status.sh",
		"instance-profile", "us-east-1", wantOut, "/dev/null",
		"--profile", "instance-profile",
		"--region", "us-east-1",
		"--output-dir", wantOut,
	}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Errorf("args mismatch:\n got %#v\nwant %#v", cmd.Args, wantArgs)
	}
	for _, want := range []string{
		"BASE=1",
		"EVIDENCE_DIR=" + wantOut,
		"AWS_PROFILE=instance-profile",
		"AWS_DEFAULT_REGION=us-east-1",
		"EXTRA=yes",
	} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("Env missing %q: %v", want, cmd.Env)
		}
	}
}
