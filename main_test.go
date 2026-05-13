package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

func TestBuildBaseEnvAutoLoadsFetcherRepoDotEnv(t *testing.T) {
	repo := t.TempDir()
	envPath := filepath.Join(repo, ".env")
	body := []byte(
		secrets.KeyParamifyUploadAPIToken + "=from-dotenv\n" +
			"GITLAB_PROJECT_1_URL=https://gitlab.example.com\n" +
			"GITLAB_PROJECT_1_API_ACCESS_TOKEN=glpat-test\n" +
			"GITLAB_PROJECT_1_ID=group/project\n" +
			"GITLAB_PROJECT_1_FETCHERS=gitlab_project_summary\n",
	)
	if err := os.WriteFile(envPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	env, loaded, err := buildBaseEnv([]string{"PATH=/usr/bin"}, "", repo, true)
	if err != nil {
		t.Fatalf("buildBaseEnv error: %v", err)
	}
	if loaded != envPath {
		t.Fatalf("loaded path: got %q want %q", loaded, envPath)
	}
	if got := envValue(env, secrets.KeyParamifyUploadAPIToken); got != "from-dotenv" {
		t.Fatalf("paramify token: got %q", got)
	}
	if got := envValue(env, "GITLAB_PROJECT_1_API_ACCESS_TOKEN"); got != "glpat-test" {
		t.Fatalf("gitlab token: got %q", got)
	}
}

func TestBuildBaseEnvProcessEnvWinsOverDotEnv(t *testing.T) {
	repo := t.TempDir()
	envPath := filepath.Join(repo, ".env")
	if err := os.WriteFile(envPath, []byte("AWS_PROFILE=from-dotenv\nAWS_DEFAULT_REGION=us-east-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, _, err := buildBaseEnv([]string{"AWS_PROFILE=from-shell"}, "", repo, true)
	if err != nil {
		t.Fatalf("buildBaseEnv error: %v", err)
	}
	if got := envValue(env, "AWS_PROFILE"); got != "from-shell" {
		t.Fatalf("process env should win, got %q", got)
	}
	if got := envValue(env, "AWS_DEFAULT_REGION"); got != "us-east-1" {
		t.Fatalf("dotenv region should be added, got %q", got)
	}
}

func TestBuildBaseEnvSkipsMissingAutoDotEnv(t *testing.T) {
	env, loaded, err := buildBaseEnv([]string{"PATH=/usr/bin"}, "", t.TempDir(), true)
	if err != nil {
		t.Fatalf("buildBaseEnv error: %v", err)
	}
	if loaded != "" {
		t.Fatalf("unexpected loaded env path %q", loaded)
	}
	if got := envValue(env, "PATH"); got != "/usr/bin" {
		t.Fatalf("base env not preserved, got %q", got)
	}
}
