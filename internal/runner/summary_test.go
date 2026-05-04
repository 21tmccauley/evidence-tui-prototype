package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummaryWriter_MatchesPythonShape(t *testing.T) {
	dir := t.TempDir()

	stdOut := filepath.Join(dir, "s3_encryption_status")
	if err := os.MkdirAll(stdOut, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(stdOut, "s3_encryption_status.json"), `{"metadata":{"region":"us-east-1"}}`)

	w := SummaryWriter{Now: func() time.Time { return time.Date(2026, 5, 4, 9, 0, 0, 123456000, time.UTC) }}
	err := w.WriteSummary(dir, []SummaryResult{
		{
			CheckName:   "s3_encryption_status",
			ScriptKey:   "s3_encryption_status",
			Instance:    Instance{ID: "EVD-S3-ENC", BaseID: "EVD-S3-ENC"},
			Success:     true,
			ErrorReason: "",
		},
	})
	if err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}

	if got["timestamp"] != "2026-05-04T09:00:00.123456Z" {
		t.Fatalf("timestamp: got %v", got["timestamp"])
	}
	if got["evidence_directory"] != dir {
		t.Fatalf("evidence_directory: got %v want %v", got["evidence_directory"], dir)
	}
	if got["total_scripts"] != float64(1) || got["successful_scripts"] != float64(1) || got["failed_scripts"] != float64(0) {
		t.Fatalf("counts: %#v", got)
	}

	results, ok := got["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results shape: %#v", got["results"])
	}
	row := results[0].(map[string]any)
	if row["check"] != "s3_encryption_status" {
		t.Fatalf("check: %#v", row)
	}
	if row["status"] != "PASS" {
		t.Fatalf("status: %#v", row)
	}
	if row["resource"] != "us-east-1" {
		t.Fatalf("resource should fall back from metadata, got %#v", row)
	}
	if row["evidence_file"] == nil {
		t.Fatalf("expected evidence_file path, got nil")
	}
}

// Pins summary.json bytes against run_fetchers.py:create_summary_file (indent, key order, FAIL rows).
func TestSummaryWriter_ByteShape_MatchesPython(t *testing.T) {
	dir := t.TempDir()

	for _, sub := range []string{"ok_check", "fail_check"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	writeFile(t, filepath.Join(dir, "ok_check", "ok_check.json"),
		`{"metadata":{"region":"us-west-2"}}`)

	w := SummaryWriter{Now: func() time.Time { return time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC) }}
	if err := w.WriteSummary(dir, []SummaryResult{
		{
			CheckName: "ok_check",
			ScriptKey: "ok_check",
			Instance:  Instance{ID: "EVD-OK", BaseID: "EVD-OK"},
			Success:   true,
		},
		{
			CheckName:   "fail_check",
			ScriptKey:   "fail_check",
			Instance:    Instance{ID: "EVD-BAD", BaseID: "EVD-BAD"},
			Success:     false,
			ErrorReason: "exit 7: missing IAM permission",
		},
	}); err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}

	if !bytes.Contains(raw, []byte("\n  \"timestamp\":")) {
		t.Errorf("summary.json must use 2-space indent at the top level, got:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("\n      \"check\":")) {
		t.Errorf("summary.json must indent result-row keys by 6 spaces, got:\n%s", raw)
	}

	wantOrder := []string{
		"\"timestamp\":",
		"\"evidence_directory\":",
		"\"total_scripts\":",
		"\"successful_scripts\":",
		"\"failed_scripts\":",
		"\"results\":",
	}
	last := -1
	for _, key := range wantOrder {
		idx := bytes.Index(raw, []byte(key))
		if idx < 0 {
			t.Errorf("missing key %s in summary.json:\n%s", key, raw)
			continue
		}
		if idx <= last {
			t.Errorf("key %s appears before earlier key (got idx %d, last %d)", key, idx, last)
		}
		last = idx
	}

	if !bytes.Contains(raw, []byte(`"timestamp": "2026-05-04T09:00:00Z"`)) {
		t.Errorf("zero-microsecond timestamp should drop the fractional, got:\n%s", raw)
	}

	if !bytes.Contains(raw, []byte(`"check": "ok_check"`)) ||
		!bytes.Contains(raw, []byte(`"status": "PASS"`)) ||
		!bytes.Contains(raw, []byte(`"resource": "us-west-2"`)) {
		t.Errorf("PASS row missing expected fields:\n%s", raw)
	}

	if !bytes.Contains(raw, []byte(`"check": "fail_check"`)) ||
		!bytes.Contains(raw, []byte(`"status": "FAIL"`)) ||
		!bytes.Contains(raw, []byte(`"evidence_file": null`)) ||
		!bytes.Contains(raw, []byte(`"error_reason": "exit 7: missing IAM permission"`)) {
		t.Errorf("FAIL row missing expected fields (error_reason, null evidence_file):\n%s", raw)
	}

	if !bytes.Contains(raw, []byte(`"total_scripts": 2`)) ||
		!bytes.Contains(raw, []byte(`"successful_scripts": 1`)) ||
		!bytes.Contains(raw, []byte(`"failed_scripts": 1`)) {
		t.Errorf("counts wrong:\n%s", raw)
	}

	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Errorf("summary.json should end with a newline")
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	rows := decoded["results"].([]any)
	if len(rows) != 2 {
		t.Fatalf("expected 2 result rows, got %d", len(rows))
	}
	failRow := rows[1].(map[string]any)
	if _, present := failRow["evidence_file"]; !present {
		t.Errorf("failed row should still contain the evidence_file key (as null), got: %#v", failRow)
	}
	if v, ok := failRow["evidence_file"]; ok && v != nil {
		t.Errorf("failed row evidence_file should be null, got %#v", v)
	}
	if !strings.Contains(string(raw), "exit 7:") {
		t.Errorf("error_reason text should pass through verbatim, got:\n%s", raw)
	}
}
