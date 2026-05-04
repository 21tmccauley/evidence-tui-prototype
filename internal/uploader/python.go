package uploader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const paramifyPusherPath = "2-create-evidence-sets/paramify_pusher.py"

// PythonConfig wires the existing evidence-fetchers Python upload path.
type PythonConfig struct {
	FetcherRepoRoot string
	Python          string
	BaseURL         string
	Environ         []string
}

// PythonUploader delegates upload to evidence-fetchers/2-create-evidence-sets/paramify_pusher.py.
type PythonUploader struct {
	fetcherRepoRoot string
	python          string
	baseURL         string
	environ         []string
}

// NewPython returns an uploader backed by the existing Python Paramify pusher.
func NewPython(cfg PythonConfig) (*PythonUploader, error) {
	root := strings.TrimSpace(cfg.FetcherRepoRoot)
	if root == "" {
		return nil, errors.New("python uploader: empty fetcher repo root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("python uploader: fetcher repo root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("python uploader: fetcher repo root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("python uploader: fetcher repo root %q is not a directory", abs)
	}
	python := strings.TrimSpace(cfg.Python)
	if python == "" {
		python = "python3"
	}
	return &PythonUploader{
		fetcherRepoRoot: abs,
		python:          python,
		baseURL:         strings.TrimSpace(cfg.BaseURL),
		environ:         cloneEnv(cfg.Environ),
	}, nil
}

func (u *PythonUploader) ProcessEvidenceDir(ctx context.Context, dir string) (Summary, error) {
	if u == nil {
		return Summary{}, errors.New("python uploader is nil")
	}
	runDir := strings.TrimSpace(dir)
	if runDir == "" {
		return Summary{}, errors.New("python uploader: empty evidence dir")
	}
	absRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return Summary{}, fmt.Errorf("python uploader: evidence dir: %w", err)
	}
	summaryPath := filepath.Join(absRunDir, "summary.json")
	if _, err := os.Stat(summaryPath); err != nil {
		return Summary{}, fmt.Errorf("python uploader: summary.json: %w", err)
	}

	scriptPath := filepath.Join(u.fetcherRepoRoot, filepath.FromSlash(paramifyPusherPath))
	if _, err := os.Stat(scriptPath); err != nil {
		return Summary{}, fmt.Errorf("python uploader: paramify_pusher.py: %w", err)
	}

	logPath := filepath.Join(absRunDir, "upload_log.json")
	args := []string{scriptPath, summaryPath, "--log-file", logPath}
	if u.baseURL != "" {
		args = append(args, "--base-url", u.baseURL)
	}

	cmd := exec.CommandContext(ctx, u.python, args...)
	cmd.Dir = u.fetcherRepoRoot
	cmd.Env = u.environment()

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()
	sum, logErr := readPythonUploadLog(logPath)
	if runErr != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return sum, fmt.Errorf("python uploader failed: %s", msg)
	}
	if logErr != nil {
		return sum, logErr
	}
	return sum, nil
}

func (u *PythonUploader) environment() []string {
	env := cloneEnv(u.environ)
	if env == nil {
		env = os.Environ()
	}
	return env
}

func cloneEnv(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func readPythonUploadLog(path string) (Summary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, fmt.Errorf("python uploader: read upload_log.json: %w", err)
	}
	var env uploadLogEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return Summary{}, fmt.Errorf("python uploader: parse upload_log.json: %w", err)
	}
	sum := Summary{Results: env.Results}
	for _, row := range env.Results {
		if row.UploadSuccess {
			sum.Successful++
		} else {
			sum.FailedUploads++
		}
	}
	return sum, nil
}
