package uploader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

type contractSender struct {
	done chan runner.FinishedMsg
	once sync.Once
}

func (s *contractSender) Send(msg tea.Msg) {
	if fm, ok := msg.(runner.FinishedMsg); ok {
		s.once.Do(func() { s.done <- fm })
	}
}

func TestMVPContract_RealRunnerOutputFeedsPythonUploaderBridge(t *testing.T) {
	requireTool(t, "bash")
	requireTool(t, "sh")

	repo := t.TempDir()
	fetcherPath := filepath.Join(repo, "fetchers", "aws", "echo_ok.sh")
	if err := os.MkdirAll(filepath.Dir(fetcherPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fetcherPath, []byte(`#!/bin/bash
PROFILE="$1"
REGION="$2"
OUTPUT_DIR="$3"
echo "ok-line"
mkdir -p "$OUTPUT_DIR"
cat > "$OUTPUT_DIR/echo_ok.json" <<JSON
{
  "metadata": {
    "profile": "$PROFILE",
    "region": "$REGION",
    "account_id": "111122223333",
    "arn": "arn:aws:iam::111122223333:role/test"
  },
  "results": []
}
JSON
`), 0o755); err != nil {
		t.Fatal(err)
	}

	pusherPath := filepath.Join(repo, "2-create-evidence-sets", "paramify_pusher.py")
	if err := os.MkdirAll(filepath.Dir(pusherPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pusherPath, []byte("# placeholder; fake python handles execution\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runDir := t.TempDir()
	const id runner.FetcherID = "EVD-TEST-MVP"
	r := runner.NewReal(runner.Config{
		Profile:                "test-profile",
		Region:                 "us-east-1",
		FetcherRepoRoot:        repo,
		OutputRoot:             runDir,
		EvidenceSetsCompatPath: filepath.Join(repo, "evidence_sets.json"),
		Scripts: map[runner.FetcherID]catalog.Script{
			id: {
				ID:           string(id),
				Name:         "MVP Contract",
				Description:  "Exercises real runner to Python upload bridge",
				ScriptFile:   "fetchers/aws/echo_ok.sh",
				Source:       "test",
				Key:          "echo_ok",
				Instructions: "Script: echo_ok.sh. Commands executed: echo",
			},
		},
	})
	sender := &contractSender{done: make(chan runner.FinishedMsg, 1)}
	r.Bind(sender)

	if msg := r.Start([]runner.FetcherID{id})(); msg != nil {
		if _, cmd := r.Update(msg); cmd != nil {
			cmd()
		}
	}
	select {
	case fm := <-sender.done:
		if fm.Status != runner.StatusOK {
			t.Fatalf("runner status: got %s reason=%q", fm.Status, fm.ErrorReason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for runner")
	}

	summaryPath := filepath.Join(runDir, "summary.json")
	summaryBytes, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summaryBytes), filepath.Join(runDir, "echo_ok", "echo_ok.json")) {
		t.Fatalf("summary.json must point at the fetcher evidence file, got:\n%s", summaryBytes)
	}
	if _, err := os.Stat(filepath.Join(repo, "evidence_sets.json")); err != nil {
		t.Fatalf("repo-root evidence_sets.json missing: %v", err)
	}

	fakePython := filepath.Join(t.TempDir(), "fake-python")
	if err := os.WriteFile(fakePython, []byte(`#!/bin/sh
summary="$2"
log=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--log-file" ]; then
    shift
    log="$1"
  fi
  shift
done
if ! grep -q "echo_ok.json" "$summary"; then
  echo "summary did not contain evidence file" >&2
  exit 1
fi
if [ ! -f "evidence_sets.json" ]; then
  echo "repo-root evidence_sets.json missing" >&2
  exit 1
fi
cat > "$log" <<JSON
{
  "upload_timestamp": "2026-05-04T00:00:00Z",
  "results": [
    {
      "check": "echo_ok",
      "resource": "unknown",
      "status": "PASS",
      "evidence_file": "echo_ok.json",
      "evidence_set_id": "set-1",
      "upload_success": true,
      "timestamp": "2026-05-04T00:00:00Z"
    }
  ]
}
JSON
`), 0o755); err != nil {
		t.Fatal(err)
	}

	u, err := NewPython(PythonConfig{FetcherRepoRoot: repo, Python: fakePython})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := u.ProcessEvidenceDir(context.Background(), runDir)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Successful != 1 || sum.FailedUploads != 0 || len(sum.Results) != 1 {
		t.Fatalf("upload summary: %#v", sum)
	}
	if _, err := os.Stat(filepath.Join(runDir, "upload_log.json")); err != nil {
		t.Fatalf("upload_log.json missing: %v", err)
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH: %v", name, err)
	}
}
