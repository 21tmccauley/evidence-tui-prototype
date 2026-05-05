package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/secrets"
)

func TestSecretsScreen_ReadOnlyBackendDisablesEdit(t *testing.T) {
	store := secrets.Env{Environ: []string{secrets.KeyKnowBe4APIKey + "=from-env"}}
	m := NewSecrets(app.DefaultKeys(), store)
	m = m.Resize(140, 40)

	if cmd := m.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			m, _ = m.Update(msg)
		}
	}

	if v := m.View(); !strings.Contains(v, "read-only backend") {
		t.Fatalf("expected read-only banner in view, got:\n%s", v)
	}

	// Drill into paramify keys, then attempt to edit; read-only should
	// surface a status message and never enter edit mode.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // sources -> keys
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // edit attempt
	if m.editing {
		t.Fatalf("read-only backend must not enter edit mode on enter")
	}
	if !strings.Contains(m.View(), "read-only") {
		t.Fatalf("expected read-only status after enter press, got:\n%s", m.View())
	}
}

// FocusKeys is the path Review uses to nag for the Paramify upload token.
// It collapses the screen into a single section listing only those keys.
// Provenance ("set (memory)") still has to render.
func TestSecretsScreen_FocusedModeShowsProvenance(t *testing.T) {
	mem := secrets.NewMemory()
	if err := mem.Set(secrets.KeyKnowBe4APIKey, "abc"); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	store := secrets.Merged{
		Primary:  mem,
		Fallback: secrets.Env{Environ: []string{}},
		Writer:   mem,
	}
	m := NewSecretsWithOptions(app.DefaultKeys(), store, SecretsOptions{
		FocusKeys: []string{secrets.KeyKnowBe4APIKey},
	})
	m = m.Resize(140, 40)

	if cmd := m.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			m, _ = m.Update(msg)
		}
	}

	if v := m.View(); !strings.Contains(v, "set (memory)") {
		t.Fatalf("expected provenance 'set (memory)' in view, got:\n%s", v)
	}
}

// The default (non-focused) Secrets screen lists Paramify pinned first and
// every catalog source after it. Sources without env-var creds (aws, k8s,
// ssllabs, …) render an info row.
func TestSecretsScreen_ListsParamifyPlusCatalogSources(t *testing.T) {
	mem := secrets.NewMemory()
	m := NewSecrets(app.DefaultKeys(), mem)
	m = m.Resize(140, 40)
	if cmd := m.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			m, _ = m.Update(msg)
		}
	}

	v := m.View()
	if !strings.Contains(v, "Paramify") {
		t.Fatalf("expected Paramify pinned source, got:\n%s", v)
	}
	for _, src := range mock.Sources(mock.Catalog()) {
		want := secrets.SecretsForSource(src).Label
		if !strings.Contains(v, want) {
			t.Fatalf("expected source %q (label %q) in left pane, got:\n%s", src, want, v)
		}
	}
	if !strings.Contains(v, "(info)") {
		t.Fatalf("expected at least one info-only source (e.g. aws) marked '(info)', got:\n%s", v)
	}
}
