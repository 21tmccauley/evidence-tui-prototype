package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHome_PrefersExplicitOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PARAMIFY_FETCHER_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "should-be-ignored"))
	t.Setenv("HOME", filepath.Join(tmp, "also-ignored"))

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != tmp {
		t.Fatalf("Home: got %q, want %q", got, tmp)
	}
}

func TestHome_FallsBackToXDGDataHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PARAMIFY_FETCHER_HOME", "")
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", filepath.Join(tmp, "ignored"))

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	want := filepath.Join(tmp, AppDirName)
	if got != want {
		t.Fatalf("Home: got %q, want %q", got, want)
	}
}

func TestHome_FallsBackToUserHomeDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PARAMIFY_FETCHER_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", tmp)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	want := filepath.Join(tmp, ".local", "share", AppDirName)
	if got != want {
		t.Fatalf("Home: got %q, want %q", got, want)
	}
}

func TestRunTimestamp_IsUTCAndFilesystemSafe(t *testing.T) {
	// 19:22:04 in UTC+02:00 should render as 17:22:04 UTC.
	loc := time.FixedZone("EET", 2*60*60)
	stamp := time.Date(2026, 5, 4, 19, 22, 4, 0, loc)
	got := RunTimestamp(stamp)
	want := "2026-05-04T17-22-04Z"
	if got != want {
		t.Fatalf("RunTimestamp: got %q want %q", got, want)
	}
	// No characters that are awkward on Windows or shell quoting.
	for _, ch := range got {
		switch ch {
		case ':', '/', '\\', ' ':
			t.Fatalf("RunTimestamp returned %q with reserved char %q", got, ch)
		}
	}
}

func TestEnsureRunDir_CreatesUnderEvidenceRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PARAMIFY_FETCHER_HOME", tmp)

	ts := RunTimestamp(time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC))
	dir, err := EnsureRunDir(ts)
	if err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	want := filepath.Join(tmp, "evidence", ts)
	if dir != want {
		t.Fatalf("dir: got %q want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory to exist after EnsureRunDir")
	}
	// Idempotent.
	if _, err := EnsureRunDir(ts); err != nil {
		t.Fatalf("second EnsureRunDir should be a no-op, got %v", err)
	}
}

func TestEnsureLogsDir_AndSessionLogPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PARAMIFY_FETCHER_HOME", tmp)

	dir, err := EnsureLogsDir()
	if err != nil {
		t.Fatalf("EnsureLogsDir: %v", err)
	}
	if dir != filepath.Join(tmp, "logs") {
		t.Fatalf("logs dir: got %q", dir)
	}

	ts := "2026-05-04T09-00-00Z"
	got, err := SessionLogPath(ts)
	if err != nil {
		t.Fatalf("SessionLogPath: %v", err)
	}
	want := filepath.Join(tmp, "logs", "session-"+ts+".log")
	if got != want {
		t.Fatalf("SessionLogPath: got %q want %q", got, want)
	}
	if !strings.HasSuffix(got, ".log") {
		t.Fatal("session log path must have .log extension")
	}
}
