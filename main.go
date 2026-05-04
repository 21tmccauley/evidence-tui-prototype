package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/output"
	"github.com/paramify/evidence-tui-prototype/internal/preflight"
	"github.com/paramify/evidence-tui-prototype/internal/root"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
	"github.com/paramify/evidence-tui-prototype/internal/screens"
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

	sessionLog.Logf("paramify-fetcher start demo=%t catalog=%q profile=%q region=%q output-root=%q",
		*demo, *catalogPath, *profile, *region, *outputRoot)

	var (
		r              runner.Runner
		welcomeOpts    screens.WelcomeOptions
		evidenceDir    string
		paramifyClient uploader.Uploader
	)
	if *demo {
		r = mock.NewMockRunner(mock.Catalog())
	} else {
		var repoAbs string
		r, evidenceDir, repoAbs = buildRealRunner(*profile, *region, *repoRoot, *catalogPath, *outputRoot, runTS)
		welcomeOpts = realWelcomeOptions(*profile, *region)
		sessionLog.Logf("evidence directory: %s", evidenceDir)
		c, err := uploader.NewPython(uploader.PythonConfig{
			FetcherRepoRoot: repoAbs,
			BaseURL:         os.Getenv("PARAMIFY_API_BASE_URL"),
		})
		if err != nil {
			sessionLog.Logf("python uploader unavailable: %v", err)
		} else {
			paramifyClient = c
		}
	}

	rootModel := root.NewWithOptions(r, root.Options{
		Welcome:     welcomeOpts,
		EvidenceDir: evidenceDir,
		Paramify:    paramifyClient,
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

func realWelcomeOptions(profile, region string) screens.WelcomeOptions {
	return screens.WelcomeOptions{
		Profiles:    loadWelcomeProfiles(profile, region),
		Tools:       preflight.CheckTools([]string{"aws", "jq", "bash", "python3", "kubectl", "curl"}),
		Credential:  &preflight.Service{Cache: preflightCachePath, Checker: runner.CLIAuthChecker{}},
		InitialName: profile,
	}
}

func loadWelcomeProfiles(profile, region string) []screens.Profile {
	awsProfiles, err := preflight.LoadAWSProfiles("")
	profiles := make([]screens.Profile, 0, len(awsProfiles)+1)
	for _, p := range awsProfiles {
		profiles = append(profiles, screens.Profile{
			Name:   p.Name,
			Region: firstNonEmpty(p.Region, region, os.Getenv("AWS_DEFAULT_REGION"), os.Getenv("AWS_REGION"), "—"),
			Note:   p.Note,
		})
	}
	if profile != "" && !hasProfile(profiles, profile) {
		profiles = append([]screens.Profile{{
			Name:   profile,
			Region: firstNonEmpty(region, os.Getenv("AWS_DEFAULT_REGION"), os.Getenv("AWS_REGION"), "—"),
			Note:   "from --profile",
		}}, profiles...)
	}
	if len(profiles) == 0 || err != nil {
		name := firstNonEmpty(profile, os.Getenv("AWS_PROFILE"), "default")
		profiles = []screens.Profile{{
			Name:   name,
			Region: firstNonEmpty(region, os.Getenv("AWS_DEFAULT_REGION"), os.Getenv("AWS_REGION"), "—"),
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
func buildRealRunner(profile, region, repoRoot, catalogPath, outputRootFlag, runTS string) (runner.Runner, string, string) {
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
	}), evidenceDir, repoAbs
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
