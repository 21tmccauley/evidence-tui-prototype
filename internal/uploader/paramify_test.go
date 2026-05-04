package uploader

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paramify/evidence-tui-prototype/internal/evidence"
)

func TestEvidenceSetForCheck_InstanceSuffix(t *testing.T) {
	doc := evidence.Document{
		EvidenceSets: map[string]evidence.Set{
			"checkov_terraform": {ID: "EVD-1", Name: "Checkov Terraform"},
		},
	}
	s, ok := evidenceSetForCheck("checkov_terraform_project_2", doc)
	if !ok || s.ID != "EVD-1" {
		t.Fatalf("expected base match, got ok=%v %#v", ok, s)
	}
}

func TestBuildArtifactTitle(t *testing.T) {
	if g := buildArtifactTitle("x", "unknown", "S3"); g != "S3" {
		t.Fatalf("got %q", g)
	}
	if g := buildArtifactTitle("x", "my/repo", "Checkov"); g != "Checkov - my/repo" {
		t.Fatalf("got %q", g)
	}
}

func TestParseEvidenceIDFromList(t *testing.T) {
	body := []byte(`{"evidences":[{"id":"abc","referenceId":"EVD-X"}]}`)
	id, ok, err := parseEvidenceIDFromList(body, "EVD-X")
	if err != nil || !ok || id != "abc" {
		t.Fatalf("got id=%q ok=%v err=%v", id, ok, err)
	}
	_, ok, _ = parseEvidenceIDFromList(body, "EVD-MISS")
	if ok {
		t.Fatal("expected miss")
	}
}

func TestClient_FindEvidenceSet_QueryThenFallback(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/evidence" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("referenceId") {
		case "EVD-FILTER":
			_, _ = w.Write([]byte(`{"evidences":[{"id":"f1","referenceId":"EVD-FILTER"}]}`))
		default:
			_, _ = w.Write([]byte(`{"evidences":[{"id":"f2","referenceId":"EVD-LIST"}]}`))
		}
	}))
	defer ts.Close()

	c, err := New(Config{Token: "tok", BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, ok, err := c.findEvidenceSetID(ctx, "EVD-FILTER")
	if err != nil || !ok || id != "f1" {
		t.Fatalf("filter path: id=%q ok=%v err=%v", id, ok, err)
	}
	id, ok, err = c.findEvidenceSetID(ctx, "EVD-LIST")
	if err != nil || !ok || id != "f2" {
		t.Fatalf("fallback list: id=%q ok=%v err=%v calls=%d", id, ok, err, calls.Load())
	}
}

func TestClient_CreateEvidenceSet_400DuplicateThenLookup(t *testing.T) {
	var posts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/evidence":
			if posts.Load() == 0 {
				_, _ = w.Write([]byte(`{"evidences":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"evidences":[{"id":"existing","referenceId":"EVD-DUP"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/evidence":
			if posts.Add(1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"Reference ID already exists"}`))
				return
			}
			t.Fatalf("unexpected second POST")
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c, err := New(Config{Token: "tok", BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	set := evidence.Set{
		ID:           "EVD-DUP",
		Name:         "N",
		Description:  "D",
		Instructions: []evidence.RichNode{{Type: "p", Children: []evidence.RichNode{{Text: "x"}}}},
	}
	id, err := c.getOrCreateEvidenceSet(context.Background(), set)
	if err != nil || id != "existing" {
		t.Fatalf("got id=%q err=%v", id, err)
	}
}

func TestRoundTrip_Retries429ThenOK(t *testing.T) {
	var n atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"evidences":[]}`))
	}))
	defer ts.Close()

	c, err := New(Config{Token: "t", BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, _, err = c.findEvidenceSetID(context.Background(), "EVD-X")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	// 2s + 4s between attempts before success on 3rd try.
	if elapsed < 5*time.Second {
		t.Fatalf("backoff too short: %v", elapsed)
	}
	if n.Load() < 3 {
		t.Fatalf("want at least 3 HTTP calls for 429 backoff, got %d", n.Load())
	}
}

func TestScriptArtifactExists_DedupNote(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arts := []map[string]string{{
			"originalFileName": "foo.sh",
			"title":            "foo.sh",
			"note":             ScriptArtifactNotePrefix + " foo.sh",
		}}
		b, _ := json.Marshal(arts)
		_, _ = w.Write(b)
	}))
	defer ts.Close()
	c, _ := New(Config{Token: "t", BaseURL: ts.URL})
	ok, err := c.scriptArtifactExists(context.Background(), "set1", "foo.sh")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestProcessEvidenceDir_WritesUploadLogAndUploads(t *testing.T) {
	dir := t.TempDir()
	evidenceFile := filepath.Join(dir, "out.json")
	if err := os.WriteFile(evidenceFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := map[string]any{
		"timestamp": "2026-05-04T00:00:00Z",
		"results": []map[string]any{{
			"check":         "demo_fetcher",
			"resource":      "unknown",
			"status":        "PASS",
			"evidence_file": evidenceFile,
		}},
	}
	sb, _ := json.Marshal(summary)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), sb, 0o644); err != nil {
		t.Fatal(err)
	}
	doc := evidence.Document{
		EvidenceSets: map[string]evidence.Set{
			"demo_fetcher": {
				ID:           "EVD-TEST",
				Name:         "Demo",
				Description:  "d",
				ScriptFile:   "fetchers/aws/echo_ok.sh",
				Instructions: []evidence.RichNode{{Type: "p", Children: []evidence.RichNode{{Text: "hi"}}}},
			},
		},
	}
	eb, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, "evidence_sets.json"), eb, 0o644); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "fetchers", "aws", "echo_ok.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var uploads atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/evidence":
			q := r.URL.Query().Get("referenceId")
			if q == "EVD-TEST" {
				_, _ = w.Write([]byte(`{"evidences":[{"id":"sid","referenceId":"EVD-TEST"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"evidences":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/artifacts/upload"):
			uploads.Add(1)
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "multipart/form-data") {
				t.Fatalf("content-type: %q", ct)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			if r.MultipartForm.File["file"] == nil {
				t.Fatal("missing file part")
			}
			fh := r.MultipartForm.File["file"][0]
			f, _ := fh.Open()
			b, _ := io.ReadAll(f)
			_ = f.Close()
			switch fh.Filename {
			case "out.json":
				if string(b) != "{}" {
					t.Errorf("evidence file body %q", b)
				}
			case "echo_ok.sh":
				// script artifact upload
			default:
				t.Errorf("unexpected upload filename %q", fh.Filename)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl, err := New(Config{
		Token:           "secret-token",
		BaseURL:         ts.URL,
		FetcherRepoRoot: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := cl.ProcessEvidenceDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Successful != 1 || len(sum.Results) != 1 || !sum.Results[0].UploadSuccess {
		t.Fatalf("unexpected summary: %+v err=%v", sum, err)
	}
	if uploads.Load() < 1 {
		t.Fatalf("expected artifact upload, got %d", uploads.Load())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "upload_log.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"upload_timestamp"`) || !strings.Contains(string(raw), `"results"`) {
		t.Fatalf("upload_log.json: %s", raw)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Fatal("token leaked into upload log")
	}
}

func TestProcessEvidenceDir_LogOnReadError(t *testing.T) {
	dir := t.TempDir()
	cl, _ := New(Config{Token: "x", BaseURL: "http://127.0.0.1:9"})
	_, err := cl.ProcessEvidenceDir(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "upload_log.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "read summary") {
		t.Fatalf("log: %s", raw)
	}
}
