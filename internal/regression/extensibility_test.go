// Package regression hosts end-to-end tests that lock in invariants
// spanning multiple packages. The TUI is filesystem-driven and must never
// know about a specific platform by name — this test is the executable
// definition of that contract.
package regression

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/platforms"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
	"github.com/paramify/evidence-tui-prototype/internal/screens"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

// TestNovelPlatformAppearsWithZeroGoChanges drops a synthetic platform the
// TUI has never heard of into a fake fetcher repo, then walks it through
// every consumer:
//
//  1. platforms.Discover finds the platform with the declared display name,
//     env keys, and fetchers.
//  2. The Select screen renders the platform and its fetcher.
//  3. The Secrets screen groups the env keys under the platform's display
//     name.
//  4. The runner executes the platform's Python script and reports success.
//
// Any future change that re-couples the TUI to a specific platform (an
// `if source == "aws"` branch, a hardcoded source list, etc.) will fail
// here because acme_widget does not match anything.
func TestNovelPlatformAppearsWithZeroGoChanges(t *testing.T) {
	repo := makeAcmeWidgetRepo(t)

	// (1) Discovery
	plats, err := platforms.Discover(repo)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(plats) != 1 {
		t.Fatalf("want 1 platform, got %d (%+v)", len(plats), plats)
	}
	p := plats[0]
	if p.ID != "acme_widget" {
		t.Errorf("ID = %q, want acme_widget", p.ID)
	}
	if p.DisplayName != "Acme Widget Inc." {
		t.Errorf("DisplayName = %q, want 'Acme Widget Inc.'", p.DisplayName)
	}
	if got := envKeyNames(p.EnvKeys); !equalStrings(got, []string{"ACME_API_KEY", "ACME_REGION"}) {
		t.Errorf("env keys = %v", got)
	}
	if len(p.Fetchers) != 1 || p.Fetchers[0].Name != "Widget Check" {
		t.Errorf("fetchers = %+v", p.Fetchers)
	}

	scripts := platforms.Join(repo, plats, nil)
	if len(scripts) != 1 {
		t.Fatalf("Join: want 1 script, got %d", len(scripts))
	}
	scriptID := runner.FetcherID(scripts[0].ID)
	fetchersForUI := mock.FetchersFromScripts(scripts)

	// (2) Select screen surfaces the platform (by source ID for now —
	// Select still uses mock.Fetcher.Source rather than the Platform
	// display name; threading display names through is a follow-up) and
	// the humanized fetcher name.
	sel := screens.NewSelectWithOptions(app.DefaultKeys(), screens.SelectOptions{
		Fetchers: fetchersForUI,
	}).Resize(140, 40)
	selView := sel.View()
	for _, want := range []string{"acme_widget", "Widget Check"} {
		if !strings.Contains(selView, want) {
			t.Errorf("Select screen missing %q, got:\n%s", want, selView)
		}
	}

	// (3) Secrets screen: the left pane shows the platform display name,
	// the right pane shows the focused source's keys. Navigate down past
	// the pinned Paramify entry to put acme_widget in focus, then assert
	// its declared keys render.
	store := secrets.Env{Environ: []string{"ACME_API_KEY=present"}}
	sec := screens.NewSecretsWithOptions(app.DefaultKeys(), store, screens.SecretsOptions{
		Platforms:   plats,
		EnvFilePath: filepath.Join(repo, ".env"),
	}).Resize(140, 40)
	if cmd := sec.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			sec, _ = sec.Update(msg)
		}
	}
	if !strings.Contains(sec.View(), "Acme Widget Inc.") {
		t.Errorf("Secrets left pane missing 'Acme Widget Inc.', got:\n%s", sec.View())
	}
	sec, _ = sec.Update(tea.KeyMsg{Type: tea.KeyDown})
	secView := sec.View()
	for _, want := range []string{"Acme Widget Inc.", "ACME_API_KEY", "ACME_REGION"} {
		if !strings.Contains(secView, want) {
			t.Errorf("Secrets screen missing %q after navigating to acme_widget, got:\n%s", want, secView)
		}
	}

	// (4) Runner executes the script.
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}
	byID := map[runner.FetcherID]catalog.Script{scriptID: scripts[0]}
	r := runner.NewReal(runner.Config{
		FetcherRepoRoot: repo,
		OutputRoot:      t.TempDir(),
		Scripts:         byID,
		Environ:         os.Environ(),
		MaxParallel:     1,
	})
	cs := newCaptureSender(scriptID)
	r.Bind(cs)
	startReal(t, r, []runner.FetcherID{scriptID})

	fm := cs.waitFinished(t, 10*time.Second)
	if fm.Status != runner.StatusOK {
		t.Fatalf("Run status = %s, want ok; reason=%q", fm.Status, fm.ErrorReason)
	}
}

// makeAcmeWidgetRepo creates the synthetic fetcher repo. The platform
// is deliberately named so it cannot collide with any historical hardcoded
// source list. Adding `if source == "acme_widget"` somewhere in the TUI
// would defeat the test; the next platform name would still break.
func makeAcmeWidgetRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "fetchers", "acme_widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	files := map[string]string{
		"platform.json":  `{"display_name": "Acme Widget Inc."}`,
		".env.example":   "ACME_API_KEY=\nACME_REGION=\n",
		"widget_check.py": `#!/usr/bin/env python3
import os, sys
out = os.environ.get("EVIDENCE_DIR", ".")
print(f"acme widget ok; EVIDENCE_DIR={out}")
sys.exit(0)
`,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".py") {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return repo
}

// captureSender is a minimal Sender that signals when the target id finishes.
type captureSender struct {
	mu     sync.Mutex
	target runner.FetcherID
	done   chan runner.FinishedMsg
	once   sync.Once
}

func newCaptureSender(target runner.FetcherID) *captureSender {
	return &captureSender{
		target: target,
		done:   make(chan runner.FinishedMsg, 1),
	}
}

func (s *captureSender) Send(msg tea.Msg) {
	fm, ok := msg.(runner.FinishedMsg)
	if !ok || fm.ID != s.target {
		return
	}
	s.once.Do(func() { s.done <- fm })
}

func (s *captureSender) waitFinished(t *testing.T, d time.Duration) runner.FinishedMsg {
	t.Helper()
	select {
	case fm := <-s.done:
		return fm
	case <-time.After(d):
		t.Fatalf("timeout waiting for FinishedMsg{ID=%s}", s.target)
		return runner.FinishedMsg{}
	}
}

func startReal(t *testing.T, r *runner.RealRunner, ids []runner.FetcherID) {
	t.Helper()
	cmd := r.Start(ids)
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	_, next := r.Update(msg)
	if next != nil {
		next()
	}
}

func envKeyNames(keys []platforms.EnvKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Name
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
