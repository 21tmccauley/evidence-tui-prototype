package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

// Config is static configuration for the real runner (per-instance overrides use Instance).
type Config struct {
	Profile                string
	Region                 string
	FetcherRepoRoot        string
	OutputRoot             string
	EvidenceSetsCompatPath string
	Scripts                map[FetcherID]catalog.Script
	AuthChecker            AuthChecker
	Environ                []string
	// MaxParallel is how many fetcher subprocesses may run at once. Values below 1
	// are treated as 1 in NewReal. Default 1 avoids fetcher scripts clashing on shared tmp paths.
	MaxParallel int
}

// BuildCmd assembles the *exec.Cmd per the runner contract in
// .cursor/rules/20-runner-contract.mdc:
//
//	[bash|python3] <script> <profile> <region> <output_dir> /dev/null \
//	  --profile <profile> --region <region> --output-dir <output_dir>
//
// Empty profile or region omits the corresponding `--profile`/`--region`
// flag pair, matching run_fetchers.py:501-504. The 4 positional args are
// always present (Python passes empty strings for missing values).
//
// CWD is the fetcher repo root. Env is os.Environ() plus EVIDENCE_DIR.
//
// Per-fetcher output is <OutputRoot>/<Key>/ (DESIGN.md Part 4; EVIDENCE_DIR in env).
func BuildCmd(ctx context.Context, cfg Config, s catalog.Script) *exec.Cmd {
	return BuildInstanceCmd(ctx, cfg, s, Instance{})
}

// BuildInstanceCmd assembles the *exec.Cmd for either a standard fetcher or a
// multi-instance target. Instance Env values override the process environment
// and AWS profile/region arguments for that subprocess only.
func BuildInstanceCmd(ctx context.Context, cfg Config, s catalog.Script, inst Instance) *exec.Cmd {
	scriptPath := filepath.Join(cfg.FetcherRepoRoot, s.ScriptFile)
	outDir := OutputDirForInstance(cfg.OutputRoot, s.Key, inst)
	profile, region := EffectiveProfileRegion(cfg, inst)

	prog := "bash"
	if filepath.Ext(scriptPath) == ".py" {
		prog = "python3"
	}

	args := []string{
		scriptPath,
		profile, region, outDir, "/dev/null",
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	args = append(args, "--output-dir", outDir)

	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = cfg.FetcherRepoRoot

	env := cfg.environment()
	env = append(env, "EVIDENCE_DIR="+outDir)
	for k, v := range inst.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	return cmd
}

// FetcherOutputDir returns the per-fetcher output directory inside a run
// root. Exposed so the runner and tests stay in agreement on the layout.
func FetcherOutputDir(runRoot, scriptKey string) string {
	return filepath.Join(runRoot, scriptKey)
}

// OutputDirForInstance is the per-target directory under runRoot (script key, or instance id when expanded).
func OutputDirForInstance(runRoot, scriptKey string, inst Instance) string {
	if inst.ID == "" || inst.ID == inst.BaseID {
		return FetcherOutputDir(runRoot, scriptKey)
	}
	return filepath.Join(runRoot, string(inst.ID))
}

func (cfg Config) environment() []string {
	if cfg.Environ != nil {
		out := make([]string, len(cfg.Environ))
		copy(out, cfg.Environ)
		return out
	}
	return os.Environ()
}
