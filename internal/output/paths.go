// Package output resolves per-run evidence directories and session log paths.
// Path precedence and XDG vs config: DESIGN.md Part 4; resolution order in [Home].
package output

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const AppDirName = "paramify-fetcher"

// Home is the data root: PARAMIFY_FETCHER_HOME, else XDG_DATA_HOME/paramify-fetcher, else ~/.local/share/... (DESIGN.md Part 4).
func Home() (string, error) {
	if v := strEnv("PARAMIFY_FETCHER_HOME"); v != "" {
		return v, nil
	}
	if v := strEnv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, AppDirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("cannot determine output home: set PARAMIFY_FETCHER_HOME, XDG_DATA_HOME, or HOME")
	}
	return filepath.Join(home, ".local", "share", AppDirName), nil
}

// EvidenceRoot is <Home>/evidence.
func EvidenceRoot() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "evidence"), nil
}

// LogsRoot is <Home>/logs.
func LogsRoot() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "logs"), nil
}

// RunTimestamp is the UTC filesystem-safe run stem (DESIGN.md Part 4; Python uploader accepts this and YYYY_MM_DD variants per DESIGN.md Part 3 #22).
func RunTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15-04-05Z")
}

// EnsureRunDir creates <EvidenceRoot>/<ts> and returns its absolute path (idempotent). Pass one shared ts from the caller; do not derive ts twice.
func EnsureRunDir(ts string) (string, error) {
	root, err := EvidenceRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, ts)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create evidence dir %q: %w", dir, err)
	}
	return dir, nil
}

// EnsureLogsDir resolves the logs directory and creates it. Returns the
// absolute path so callers can compose `session-<ts>.log` against it.
func EnsureLogsDir() (string, error) {
	dir, err := LogsRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create logs dir %q: %w", dir, err)
	}
	return dir, nil
}

// SessionLogPath composes the session log path for run ts (file created in OpenSessionLog).
func SessionLogPath(ts string) (string, error) {
	dir, err := LogsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session-"+ts+".log"), nil
}

func strEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return ""
	}
	return v
}
