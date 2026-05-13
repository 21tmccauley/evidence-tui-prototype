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
	"github.com/paramify/evidence-tui-prototype/internal/preflight"
	"github.com/paramify/evidence-tui-prototype/internal/root"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
	"github.com/paramify/evidence-tui-prototype/internal/screens"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
	"github.com/paramify/evidence-tui-prototype/internal/uploader"
)

const preflightCachePath = "pre-flight-cache.json"

func main() {
	demo := flag.Bool("demo", true, "use the deterministic mock runner")
	catalogPath := flag.String("catalog", "", "override the embedded evidence_fetchers_catalog.json (development)")
	profile := flag.String("profile", "", "AWS profile (real runner)")
	region := flag.String("region", "", "AWS region (real runner)")
	repoRoot := flag.String("fetcher-repo-root", "", "path to the evidence-fetchers checkout (real runner)")
	outputRoot := flag.String("output-root", "", "explicit per-run evidence directory (overrides XDG default)")
	fetcherParallel := flag.Int("fetcher-parallel", 1, "max fetcher subprocesses at once (default 1 avoids tmp-path races until scripts are isolated)")
	secretsBackend := flag.String("secrets-backend", "merged", "secrets backend: merged|keychain|env")
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

	baseEnv, loadedEnvFile, err := buildBaseEnv(os.Environ(), *envFile, *repoRoot, !*demo)
	if err != nil {
		die(2, "env file error: %v", err)
	}
	if loadedEnvFile != "" {
		sessionLog.Logf("env file loaded: %s", loadedEnvFile)
	}

	secretStore, err := buildSecretsStore(*secretsBackend, baseEnv)
	if err != nil {
		die(2, "secrets backend error: %v", err)
	}
	runtimeEnv, err := secrets.BuildEnviron(baseEnv, secretStore, secrets.RuntimeKeys())
	if err != nil {
		die(2, "secrets setup error: %v", err)
	}

	var (
		r               runner.Runner
		welcomeOpts     screens.WelcomeOptions
		evidenceDir     string
		paramifyFactory screens.ParamifyFactory
	)
	if *demo {
		r = mock.NewMockRunner(mock.Catalog())
	} else {
		var repoAbs string
		r, evidenceDir, repoAbs = buildRealRunner(*profile, *region, *repoRoot, *catalogPath, *outputRoot, *fetcherParallel, runTS, runtimeEnv)
		welcomeOpts = realWelcomeOptions(*profile, *region, baseEnv)
		sessionLog.Logf("evidence directory: %s", evidenceDir)
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

func realWelcomeOptions(profile, region string, env []string) screens.WelcomeOptions {
	return screens.WelcomeOptions{
		Profiles:    loadWelcomeProfiles(profile, region, env),
		Tools:       preflight.CheckTools([]string{"aws", "jq", "bash", "python3", "kubectl", "curl"}),
		Credential:  &preflight.Service{Cache: preflightCachePath, Checker: runner.CLIAuthChecker{}},
		InitialName: profile,
	}
}

func loadWelcomeProfiles(profile, region string, env []string) []screens.Profile {
	awsProfiles, err := preflight.LoadAWSProfiles("")
	profiles := make([]screens.Profile, 0, len(awsProfiles)+1)
	for _, p := range awsProfiles {
		profiles = append(profiles, screens.Profile{
			Name:   p.Name,
			Region: firstNonEmpty(p.Region, region, envValue(env, "AWS_DEFAULT_REGION"), envValue(env, "AWS_REGION"), "—"),
			Note:   p.Note,
		})
	}
	if profile != "" && !hasProfile(profiles, profile) {
		profiles = append([]screens.Profile{{
			Name:   profile,
			Region: firstNonEmpty(region, envValue(env, "AWS_DEFAULT_REGION"), envValue(env, "AWS_REGION"), "—"),
			Note:   "from --profile",
		}}, profiles...)
	}
	if len(profiles) == 0 || err != nil {
		name := firstNonEmpty(profile, envValue(env, "AWS_PROFILE"), "default")
		profiles = []screens.Profile{{
			Name:   name,
			Region: firstNonEmpty(region, envValue(env, "AWS_DEFAULT_REGION"), envValue(env, "AWS_REGION"), "—"),
			Note:   "from environment/default",
		}}
	}
	return profiles
}

func hasProfile(profiles []screens.Profile, name string) bool {
	for _, p := range profiles {
		if p.Name == name {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildRealRunner validates flags, builds the real runner, and returns
// runner, evidence directory, and absolute fetcher repo root.
func buildRealRunner(profile, region, repoRoot, catalogPath, outputRootFlag string, fetcherParallel int, runTS string, env []string) (runner.Runner, string, string) {
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

	scripts, err := loadCatalogScripts(catalogPath)
	if err != nil {
		die(2, "catalog error: %v", err)
	}
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
		AuthChecker: preflight.CachedAuthChecker{Service: preflight.Service{
			Cache:   preflightCachePath,
			Checker: runner.CLIAuthChecker{},
		}},
		Environ:     env,
		MaxParallel: fetcherParallel,
	}), evidenceDir, repoAbs
}

func buildSecretsStore(backend string, env []string) (secrets.Store, error) {
	envStore := secrets.Env{Environ: env}
	keychainStore := secrets.Keychain{Service: secrets.DefaultKeychainService}
	switch backend {
	case "env":
		return envStore, nil
	case "keychain":
		return keychainStore, nil
	case "merged":
		return secrets.Merged{
			Primary:  keychainStore,
			Fallback: envStore,
			Writer:   keychainStore,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported secrets backend %q", backend)
	}
}

func buildBaseEnv(base []string, envFile, repoRoot string, live bool) ([]string, string, error) {
	out := append([]string(nil), base...)
	path := strings.TrimSpace(envFile)
	if path == "" && live && strings.TrimSpace(repoRoot) != "" {
		path = filepath.Join(repoRoot, ".env")
		if info, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return out, "", nil
			}
			return nil, "", err
		} else if info.IsDir() {
			return nil, "", fmt.Errorf("%q is a directory", path)
		}
	} else if path == "" {
		return out, "", nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	merged, err := secrets.MergeEnvFile(out, abs)
	if err != nil {
		return nil, "", err
	}
	return merged, abs, nil
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
