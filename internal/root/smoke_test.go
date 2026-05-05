package root

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/screens"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

func TestSmokeWalk(t *testing.T) {
	r := mock.NewMockRunner(mock.Catalog())
	var m tea.Model = New(r)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if v := m.View(); v == "" || !strings.Contains(v, "select a profile") {
		t.Fatalf("welcome view unexpected: %q", first(v, 80))
	}

	m, _ = m.Update(screens.SelectedProfileMsg{Profile: screens.Profile{Name: "demo", Region: "us-east-1"}})
	if v := m.View(); v == "" || !strings.Contains(v, "fetchers") {
		t.Fatalf("select view unexpected: %q", first(v, 120))
	}

	m, _ = m.Update(screens.SelectionConfirmedMsg{IDs: []mock.FetcherID{
		"EVD-KMS-ROT",
		"EVD-IAM-POLICIES",
	}})
	if v := m.View(); v == "" || !strings.Contains(v, "complete") {
		t.Fatalf("run view unexpected: %q", first(v, 120))
	}

	m, _ = m.Update(screens.RunCompleteMsg{Results: []screens.RunResult{
		{ID: "EVD-KMS-ROT", Name: "KMS Key Rotation", Source: "aws", Status: mock.StatusOK},
		{ID: "EVD-IAM-POLICIES", Name: "IAM Policies", Source: "aws", Status: mock.StatusPartial},
	}})
	if v := m.View(); v == "" || !strings.Contains(v, "ok") {
		t.Fatalf("review view unexpected: %q", first(v, 120))
	}
}

func TestRoot_TogglesHelpMenu(t *testing.T) {
	r := mock.NewMockRunner(mock.Catalog())
	var m tea.Model = New(r)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if v := m.View(); !strings.Contains(v, "keyboard help") || !strings.Contains(v, "Welcome") {
		t.Fatalf("expected help menu on welcome screen, got:\n%s", v)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if v := m.View(); strings.Contains(v, "keyboard help") {
		t.Fatalf("expected esc to close help menu, got:\n%s", v)
	}
}

func TestRoot_PassesEvidenceDirToReviewScreen(t *testing.T) {
	r := mock.NewMockRunner(mock.Catalog())
	const want = "/tmp/test-evidence/2026-05-04T09-00-00Z"
	var m tea.Model = NewWithOptions(r, Options{EvidenceDir: want})

	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(screens.SelectedProfileMsg{Profile: screens.Profile{Name: "demo", Region: "us-east-1"}})
	m, _ = m.Update(screens.SelectionConfirmedMsg{IDs: []mock.FetcherID{"EVD-KMS-ROT"}})
	m, _ = m.Update(screens.RunCompleteMsg{Results: []screens.RunResult{
		{ID: "EVD-KMS-ROT", Name: "KMS Key Rotation", Source: "aws", Status: mock.StatusOK},
	}})

	v := m.View()
	if !strings.Contains(v, want) {
		t.Fatalf("review screen should show evidence dir %q, got:\n%s", want, v)
	}
	if !strings.Contains(v, "evidence:") {
		t.Fatalf("review screen should label the evidence dir line, got:\n%s", v)
	}
}

func TestRoot_UploadWithoutTokenShowsHint(t *testing.T) {
	r := mock.NewMockRunner(mock.Catalog())
	var m tea.Model = NewWithOptions(r, Options{EvidenceDir: "/tmp/evidence-no-token"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(screens.SelectedProfileMsg{Profile: screens.Profile{Name: "demo", Region: "us-east-1"}})
	m, _ = m.Update(screens.SelectionConfirmedMsg{IDs: []mock.FetcherID{"EVD-KMS-ROT"}})
	m, _ = m.Update(screens.RunCompleteMsg{Results: []screens.RunResult{
		{ID: "EVD-KMS-ROT", Name: "KMS Key Rotation", Source: "aws", Status: mock.StatusOK},
	}})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("expected upload cmd")
	}
	msg := cmd()
	m, _ = m.Update(msg)
	um, ok := msg.(screens.UploadFinishedMsg)
	if !ok {
		t.Fatalf("expected UploadFinishedMsg, got %T", msg)
	}
	if !errors.Is(um.Err, secrets.ErrNotConfigured) {
		t.Fatalf("expected secrets.ErrNotConfigured, got %v", um.Err)
	}
	v := m.View()
	if !strings.Contains(v, "PARAMIFY_UPLOAD_API_TOKEN") {
		t.Fatalf("review view should mention token env, got:\n%s", v)
	}
}

func TestRoot_OmitsEvidenceDirWhenEmpty(t *testing.T) {
	r := mock.NewMockRunner(mock.Catalog())
	var m tea.Model = New(r)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(screens.SelectedProfileMsg{Profile: screens.Profile{Name: "demo", Region: "us-east-1"}})
	m, _ = m.Update(screens.SelectionConfirmedMsg{IDs: []mock.FetcherID{"EVD-KMS-ROT"}})
	m, _ = m.Update(screens.RunCompleteMsg{Results: []screens.RunResult{
		{ID: "EVD-KMS-ROT", Name: "KMS Key Rotation", Source: "aws", Status: mock.StatusOK},
	}})

	if strings.Contains(m.View(), "evidence:") {
		t.Fatalf("demo runs must NOT show an evidence-dir row, got:\n%s", m.View())
	}
}

// The TUI no longer gates Run on per-source secret presence: the operator
// confirms a selection and Run starts immediately. Missing keys surface as
// fetcher failures (runner.Real fail-fasts AWS preflight and KnowBe4;
// everything else fails inside the script). The operator opens Secrets
// from Run / Select to fix the key and retries.
func TestRoot_SelectionWithoutSecretsGoesStraightToRun(t *testing.T) {
	r := mock.NewMockRunner(mock.Catalog())
	mem := secrets.NewMemory()
	var m tea.Model = NewWithOptions(r, Options{Secrets: mem})

	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(screens.SelectedProfileMsg{Profile: screens.Profile{Name: "demo", Region: "us-east-1"}})
	m, _ = m.Update(screens.SelectionConfirmedMsg{IDs: []mock.FetcherID{"EVD-HIGH-RISK-TRAINING"}})

	v := m.View()
	if !strings.Contains(v, "complete") {
		t.Fatalf("expected run screen, got:\n%s", first(v, 200))
	}
	if strings.Contains(v, "missing required secrets:") {
		t.Fatalf("expected NO secrets detour for missing knowbe4 key, got:\n%s", first(v, 200))
	}
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
