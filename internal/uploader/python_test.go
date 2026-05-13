package uploader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonUploader_ProcessEvidenceDir_InvokesParamifyPusher(t *testing.T) {
	repo := t.TempDir()
	scriptPath := filepath.Join(repo, "2-create-evidence-sets", "paramify_pusher.py")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("# placeholder\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"results":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	cwdPath := filepath.Join(t.TempDir(), "cwd.txt")
	envPath := filepath.Join(t.TempDir(), "env.txt")
	fakePython := filepath.Join(t.TempDir(), "fake-python")
	fake := `#!/bin/sh
printf '%s\n' "$@" > "$ARGV_CAPTURE"
pwd > "$CWD_CAPTURE"
printf '%s\n' "$PARAMIFY_UPLOAD_API_TOKEN|$PARAMIFY_API_BASE_URL" > "$ENV_CAPTURE"
log=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--log-file" ]; then
    shift
    log="$1"
  fi
  shift
done
cat > "$log" <<'JSON'
{
  "upload_timestamp": "2026-05-04T00:00:00Z",
  "results": [
    {
      "check": "demo",
      "resource": "unknown",
      "status": "PASS",
      "evidence_file": "/tmp/demo.json",
      "evidence_set_id": "set-1",
      "upload_success": true,
      "timestamp": "2026-05-04T00:00:00Z"
    },
    {
      "check": "bad",
      "resource": "unknown",
      "status": "FAIL",
      "evidence_file": "/tmp/bad.json",
      "evidence_set_id": "set-2",
      "upload_success": false,
      "timestamp": "2026-05-04T00:00:00Z"
    }
  ]
}
JSON
`
	if err := os.WriteFile(fakePython, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	u, err := NewPython(PythonConfig{
		FetcherRepoRoot: repo,
		Python:          fakePython,
		BaseURL:         "https://api.paramify.test",
		Environ: []string{
			"ARGV_CAPTURE=" + argvPath,
			"CWD_CAPTURE=" + cwdPath,
			"ENV_CAPTURE=" + envPath,
			"PARAMIFY_UPLOAD_API_TOKEN=token-from-env",
			"PARAMIFY_API_BASE_URL=https://api.paramify.test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sum, err := u.ProcessEvidenceDir(context.Background(), runDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sum.Successful, 1; got != want {
		t.Fatalf("successful: got %d want %d", got, want)
	}
	if got, want := sum.FailedUploads, 1; got != want {
		t.Fatalf("failed uploads: got %d want %d", got, want)
	}
	if got, want := len(sum.Results), 2; got != want {
		t.Fatalf("results: got %d want %d", got, want)
	}

	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(argvBytes)), "\n")
	wantArgv := []string{
		scriptPath,
		summaryPath,
		"--log-file",
		filepath.Join(runDir, "upload_log.json"),
		"--base-url",
		"https://api.paramify.test",
	}
	if strings.Join(argv, "\x00") != strings.Join(wantArgv, "\x00") {
		t.Fatalf("argv:\ngot  %#v\nwant %#v", argv, wantArgv)
	}

	cwdBytes, err := os.ReadFile(cwdPath)
	if err != nil {
		t.Fatal(err)
	}
	gotCWD, err := filepath.EvalSymlinks(strings.TrimSpace(string(cwdBytes)))
	if err != nil {
		t.Fatal(err)
	}
	wantCWD, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if gotCWD != wantCWD {
		t.Fatalf("cwd: got %q want %q", gotCWD, wantCWD)
	}

	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(envBytes)); got != "token-from-env|https://api.paramify.test" {
		t.Fatalf("env: got %q", got)
	}
}

func TestPythonUploader_ProcessEvidenceDir_ReturnsCommandOutputOnFailure(t *testing.T) {
	repo := t.TempDir()
	scriptPath := filepath.Join(repo, "2-create-evidence-sets", "paramify_pusher.py")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("# placeholder\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runDir, "summary.json"), []byte(`{"results":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fakePython := filepath.Join(t.TempDir(), "fake-python")
	if err := os.WriteFile(fakePython, []byte("#!/bin/sh\necho missing token\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	u, err := NewPython(PythonConfig{FetcherRepoRoot: repo, Python: fakePython})
	if err != nil {
		t.Fatal(err)
	}
	_, err = u.ProcessEvidenceDir(context.Background(), runDir)
	if err == nil || !strings.Contains(err.Error(), "missing token") {
		t.Fatalf("expected command output in error, got %v", err)
	}
}
