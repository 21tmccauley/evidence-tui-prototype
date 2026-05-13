package runner

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

func TestParseMultiInstanceConfig(t *testing.T) {
	env := []string{
		"AWS_PROFILE=base-profile",
		"GITLAB_PROJECT_2_URL=https://gitlab.example.com",
		"GITLAB_PROJECT_2_API_ACCESS_TOKEN=token",
		"GITLAB_PROJECT_2_ID=cloudops/change-management",
		"GITLAB_PROJECT_2_FETCHERS=checkov_terraform, gitlab_merge_request_summary",
		"GITLAB_PROJECT_2_BRANCH=main",
		"AWS_REGION_1_REGION=us-east-1",
		"AWS_REGION_1_FETCHERS=s3_encryption_status",
	}

	cfg := ParseMultiInstanceConfig(env)
	if got, want := len(cfg.GitLabProjects), 1; got != want {
		t.Fatalf("gitlab projects: got %d want %d", got, want)
	}
	project := cfg.GitLabProjects[0]
	if project.Name != "project_2" || project.Provider != "gitlab" {
		t.Fatalf("unexpected project identity: %#v", project)
	}
	if project.Env["GITLAB_PROJECT_ID"] != "cloudops/change-management" {
		t.Errorf("project id env missing: %#v", project.Env)
	}
	if project.Env["GITLAB_BRANCH"] != "main" {
		t.Errorf("project override missing: %#v", project.Env)
	}
	if !slices.Contains(project.Fetchers, "checkov_terraform") {
		t.Errorf("project fetchers missing checkov_terraform: %#v", project.Fetchers)
	}

	if got, want := len(cfg.AWSRegions), 1; got != want {
		t.Fatalf("aws regions: got %d want %d", got, want)
	}
	region := cfg.AWSRegions[0]
	if region.Name != "region_1" || region.Resource != "us-east-1" {
		t.Fatalf("unexpected region identity: %#v", region)
	}
	if region.Env["AWS_PROFILE"] != "base-profile" {
		t.Errorf("AWS profile should fall back to AWS_PROFILE env, got %#v", region.Env)
	}
	if region.Env["AWS_DEFAULT_REGION"] != "us-east-1" || region.Env["AWS_REGION"] != "us-east-1" {
		t.Errorf("AWS region env missing: %#v", region.Env)
	}
}

func TestParseMultiInstanceConfigFromMergedDotEnv(t *testing.T) {
	env := secrets.MergeEnvValues([]string{"AWS_PROFILE=from-shell"}, map[string]string{
		"GITLAB_PROJECT_1_URL":              "https://gitlab.example.com",
		"GITLAB_PROJECT_1_API_ACCESS_TOKEN": "token-from-dotenv",
		"GITLAB_PROJECT_1_ID":               "group/project",
		"GITLAB_PROJECT_1_FETCHERS":         "gitlab_project_summary",
		"AWS_REGION_1_REGION":               "us-gov-west-1",
		"AWS_REGION_1_FETCHERS":             "s3_encryption_status",
	})

	cfg := ParseMultiInstanceConfig(env)
	if got, want := len(cfg.GitLabProjects), 1; got != want {
		t.Fatalf("gitlab projects: got %d want %d", got, want)
	}
	if got := cfg.GitLabProjects[0].Env["GITLAB_API_TOKEN"]; got != "token-from-dotenv" {
		t.Fatalf("gitlab token from merged env: got %q", got)
	}
	if got, want := len(cfg.AWSRegions), 1; got != want {
		t.Fatalf("aws regions: got %d want %d", got, want)
	}
	if got := cfg.AWSRegions[0].Env["AWS_PROFILE"]; got != "from-shell" {
		t.Fatalf("aws profile should come from merged env base, got %q", got)
	}
}

func TestCreateFetcherInstances_ReplacesCoveredBaseFetchers(t *testing.T) {
	const (
		checkovID FetcherID = "EVD-CHECKOV-TERRAFORM"
		s3ID      FetcherID = "EVD-S3-ENC"
		plainID   FetcherID = "EVD-PLAIN"
	)
	scripts := map[FetcherID]catalog.Script{
		checkovID: {ID: string(checkovID), Key: "checkov_terraform"},
		s3ID:      {ID: string(s3ID), Key: "s3_encryption_status"},
		plainID:   {ID: string(plainID), Key: "plain"},
	}
	cfg := ParseMultiInstanceConfig([]string{
		"GITLAB_PROJECT_2_URL=https://gitlab.example.com",
		"GITLAB_PROJECT_2_API_ACCESS_TOKEN=token",
		"GITLAB_PROJECT_2_ID=cloudops/change-management",
		"GITLAB_PROJECT_2_FETCHERS=checkov_terraform",
		"AWS_REGION_1_REGION=us-east-1",
		"AWS_REGION_1_FETCHERS=s3_encryption_status",
	})

	instances := CreateFetcherInstances([]FetcherID{checkovID, s3ID, plainID}, scripts, cfg)
	gotIDs := []FetcherID{}
	for _, inst := range instances {
		gotIDs = append(gotIDs, inst.ID)
	}
	wantIDs := []FetcherID{
		"EVD-CHECKOV-TERRAFORM_project_2",
		"EVD-S3-ENC_region_1",
		"EVD-PLAIN",
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("ids:\n got %v\nwant %v", gotIDs, wantIDs)
	}
	if instances[0].Env["GITLAB_PROJECT_ID"] != "cloudops/change-management" {
		t.Errorf("gitlab env missing: %#v", instances[0].Env)
	}
	if instances[1].Env["AWS_DEFAULT_REGION"] != "us-east-1" {
		t.Errorf("aws env missing: %#v", instances[1].Env)
	}
	if instances[2].Name != "" || len(instances[2].Env) != 0 {
		t.Errorf("uncovered fetcher should remain standard: %#v", instances[2])
	}
}

func TestEvidenceFileForInstance_DoesNotPrefixMatchSiblingProjects(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, filepath.Join(dir, "checkov_terraform_cloudops_change-management.json"), "{}")

	inst := Instance{
		ID:       "EVD-CHECKOV-TERRAFORM_project_2",
		BaseID:   "EVD-CHECKOV-TERRAFORM",
		Name:     "project_2",
		Provider: "gitlab",
		Resource: "paramify/govcloud-infrastructure-in-terraform",
	}
	if p, ok := EvidenceFileForInstance(dir, "checkov_terraform", inst); ok {
		t.Fatalf("expected no match for missing project 2 evidence, got %s", p)
	}
}

func TestEvidenceFileForInstance_MatchesSanitizedResource(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "checkov_terraform_paramify_govcloud-infrastructure-in-terraform.json")
	writeTempFile(t, want, "{}")

	inst := Instance{
		ID:       "EVD-CHECKOV-TERRAFORM_project_2",
		BaseID:   "EVD-CHECKOV-TERRAFORM",
		Name:     "project_2",
		Provider: "gitlab",
		Resource: "paramify/govcloud-infrastructure-in-terraform",
	}
	got, ok := EvidenceFileForInstance(dir, "checkov_terraform", inst)
	if !ok {
		t.Fatal("expected evidence file match")
	}
	if got != want {
		t.Fatalf("path: got %q want %q", got, want)
	}
}

func writeTempFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
