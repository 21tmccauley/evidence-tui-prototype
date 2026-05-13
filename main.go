package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/output"
	"github.com/paramify/evidence-tui-prototype/internal/platforms"
	"github.com/paramify/evidence-tui-prototype/internal/root"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
	"github.com/paramify/evidence-tui-prototype/internal/screens"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
	"github.com/paramify/evidence-tui-prototype/internal/uploader"
)

func main() {
	demo := flag.Bool("demo", true, "use the deterministic mock runner")
	catalogPath := flag.String("catalog", "", "override the embedded evidence_fetchers_catalog.json (development)")
	profile := flag.String("profile", "", "AWS profile passed as a positional arg to legacy .sh fetchers")
	region := flag.String("region", "", "AWS region passed as a positional arg to legacy .sh fetchers")
	repoRoot := flag.String("fetcher-repo-root", "", "path to the evidence-fetchers checkout (real runner)")
	outputRoot := flag.String("output-root", "", "explicit per-run evidence directory (overrides XDG default)")
	fetcherParallel := flag.Int("fetcher-parallel", 1, "max fetcher subprocesses at once (default 1 avoids tmp-path races until scripts are isolated)")
	envFile := flag.String("env-file", "", "dotenv file to load (live mode defaults to <fetcher-repo-root>/.env when present)")
	flag.Parse()

	if *catalogPath != "" {
		mock.SetCatalogOverride(*catalogPath)
	}

	if err := mock.EnsureCatalog(); err != nil {
		die(2, "catalog error: %v", err)
	}

	runTS := output.RunTimestamp(time.Now())

	sessionLog, err := output.OpenSessionLog(runTS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: session log unavailable: %v\n", err)
	}
	defer sessionLog.Close()

	sessionLog.Logf("paramify-fetcher start demo=%t catalog=%q profile=%q region=%q output-root=%q fetcher-parallel=%d",
		*demo, *catalogPath, *profile, *region, *outputRoot, *fetcherParallel)

	baseEnv, envFilePath, envFileLoaded, err := buildBaseEnv(os.Environ(), *envFile, *repoRoot, !*demo)
	if err != nil {
		die(2, "env file error: %v", err)
	}
	switch {
	case envFileLoaded:
		sessionLog.Logf("env file loaded: %s", envFilePath)
	case envFilePath != "":
		sessionLog.Logf("env file expected at %s but not found; continuing with process environment only", envFilePath)
	}

	// .env is the canonical secret store: values come from the dotenv file
	// merged into the process environment, and the TUI surfaces what's set
	// vs. missing without owning a separate credential store.
	secretStore := secrets.Env{Environ: baseEnv}
	runtimeEnv, err := secrets.BuildEnviron(baseEnv, secretStore, secrets.RuntimeKeys())
	if err != nil {
		die(2, "secrets setup error: %v", err)
	}

	var (
		r               runner.Runner
		welcomeOpts     screens.WelcomeOptions
		evidenceDir     string
		paramifyFactory screens.ParamifyFactory
		fetchersForUI   []mock.Fetcher
		plats           []platforms.Platform
	)
	if *demo {
		r = mock.NewMockRunner(mock.Catalog())
	} else {
		var repoAbs string
		var unifiedScripts []catalog.Script
		r, evidenceDir, repoAbs, unifiedScripts, plats = buildRealRunner(*profile, *region, *repoRoot, *catalogPath, *outputRoot, *fetcherParallel, runTS, runtimeEnv, sessionLog)
		welcomeOpts = screens.WelcomeOptions{
			Platforms:     plats,
			EnvFilePath:   envFilePath,
			EnvFileLoaded: envFileLoaded,
		}
		fetchersForUI = mock.FetchersFromScripts(unifiedScripts)
		sessionLog.Logf("evidence directory: %s", evidenceDir)
		sessionLog.Logf("fetcher catalog: %d scripts (after filesystem discovery merge)", len(unifiedScripts))
		paramifyFactory = func() (uploader.Uploader, error) {
			env, err := secrets.BuildEnviron(baseEnv, secretStore, secrets.RuntimeKeys())
			if err != nil {
				return nil, err
			}
			return uploader.NewPython(uploader.PythonConfig{
				FetcherRepoRoot: repoAbs,
				BaseURL:         firstEnvValue(env, secrets.KeyParamifyAPIBaseURL),
				Environ:         env,
			})
		}
	}

	rootModel := root.NewWithOptions(r, root.Options{
		Welcome:         welcomeOpts,
		EvidenceDir:     evidenceDir,
		ParamifyFactory: paramifyFactory,
		Secrets:         secretStore,
		Fetchers:        fetchersForUI,
		Platforms:       plats,
		EnvFilePath:     envFilePath,
	})

	p := tea.NewProgram(rootModel, tea.WithAltScreen(), tea.WithMouseCellMotion())

	r.Bind(output.SenderTap{Inner: p, Log: sessionLog})

	if _, err := p.Run(); err != nil {
		sessionLog.Logf("tea program exited with error: %v", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sessionLog.Logf("paramify-fetcher exit clean")
}

// buildRealRunner validates flags, builds the real runner, and returns
// runner, evidence directory, absolute fetcher repo root, the unified
// script list (filesystem discovery merged with the catalog), and the
// discovered platforms.
func buildRealRunner(profile, region, repoRoot, catalogPath, outputRootFlag string, fetcherParallel int, runTS string, env []string, sessionLog *output.SessionLog) (runner.Runner, string, string, []catalog.Script, []platforms.Platform) {
	if repoRoot == "" {
		die(2, "--fetcher-repo-root is required when --demo=false")
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		die(2, "--fetcher-repo-root: %v", err)
	}
	info, err := os.Stat(repoAbs)
	if err != nil || !info.IsDir() {
		die(2, "--fetcher-repo-root %q is not a directory", repoAbs)
	}

	catScripts, err := loadCatalogScripts(catalogPath)
	if err != nil {
		die(2, "catalog error: %v", err)
	}

	plats, err := platforms.Discover(repoAbs)
	if err != nil {
		sessionLog.Logf("warning: platform discovery failed: %v", err)
	} else {
		sessionLog.Logf("platform discovery: %d platforms found", len(plats))
	}
	scripts := platforms.Join(repoAbs, plats, catScripts)
	byID := make(map[runner.FetcherID]catalog.Script, len(scripts))
	for _, s := range scripts {
		byID[runner.FetcherID(s.ID)] = s
	}

	evidenceDir := resolveEvidenceDir(outputRootFlag, runTS)

	return runner.NewReal(runner.Config{
		Profile:                profile,
		Region:                 region,
		FetcherRepoRoot:        repoAbs,
		OutputRoot:             evidenceDir,
		EvidenceSetsCompatPath: filepath.Join(repoAbs, "evidence_sets.json"),
		Scripts:                byID,
		Environ:                env,
		MaxParallel:            fetcherParallel,
	}), evidenceDir, repoAbs, scripts, plats
}

// buildBaseEnv returns (mergedEnv, expectedPath, loaded, err).
//
// expectedPath is the absolute path the TUI looked at, even when the file
// is absent — so the Welcome and Secrets screens can show the user where
// the .env should live. loaded is true only when the file existed and its
// values were merged into the returned env. An explicit --env-file that
// fails to read is still an error; an auto-detected <repoRoot>/.env that
// doesn't exist is not.
func buildBaseEnv(base []string, envFile, repoRoot string, live bool) ([]string, string, bool, error) {
	out := append([]string(nil), base...)
	explicit := strings.TrimSpace(envFile)

	if explicit == "" && (!live || strings.TrimSpace(repoRoot) == "") {
		// Demo mode without --env-file: nothing to look at.
		return out, "", false, nil
	}

	var candidate string
	if explicit != "" {
		candidate = explicit
	} else {
		candidate = filepath.Join(repoRoot, ".env")
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return nil, "", false, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// Auto-detected path missing is non-fatal — surface it in the UI
			// so the user knows where to drop their .env. An explicit
			// --env-file that doesn't exist is still surfaced as the expected
			// path; the caller decides how to communicate it.
			return out, abs, false, nil
		}
		return nil, "", false, err
	}
	if info.IsDir() {
		return nil, "", false, fmt.Errorf("%q is a directory", abs)
	}

	merged, err := secrets.MergeEnvFile(out, abs)
	if err != nil {
		return nil, "", false, err
	}
	return merged, abs, true, nil
}

func firstEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) > len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
	}
	return ""
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// resolveEvidenceDir returns the per-run evidence directory: --output-root if
// set (full path, created if missing), otherwise output.EnsureRunDir(runTS).
// Precedence: DESIGN.md Part 4.
func resolveEvidenceDir(outputRootFlag, runTS string) string {
	if outputRootFlag != "" {
		abs, err := filepath.Abs(outputRootFlag)
		if err != nil {
			die(2, "--output-root: %v", err)
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			die(2, "--output-root: create %q: %v", abs, err)
		}
		return abs
	}
	dir, err := output.EnsureRunDir(runTS)
	if err != nil {
		die(2, "%v", err)
	}
	return dir
}

// loadCatalogScripts loads the embedded catalog or --catalog override (same resolution as mock.EnsureCatalog).
func loadCatalogScripts(catalogPath string) ([]catalog.Script, error) {
	if catalogPath == "" {
		_, scripts, err := catalog.LoadEmbedded()
		return scripts, err
	}
	_, scripts, err := catalog.LoadFile(catalogPath)
	return scripts, err
}

func die(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
