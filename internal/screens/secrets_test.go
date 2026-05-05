package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/app"
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

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Fatalf("read-only backend must not enter edit mode on enter")
	}
	if !strings.Contains(m.View(), "read-only") {
		t.Fatalf("expected read-only status after enter press, got:\n%s", m.View())
	}
}

func TestSecretsScreen_ProvenanceLabelMatchesBackend(t *testing.T) {
	mem := secrets.NewMemory()
	if err := mem.Set(secrets.KeyKnowBe4APIKey, "abc"); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	store := secrets.Merged{
		Primary:  mem,
		Fallback: secrets.Env{Environ: []string{}},
		Writer:   mem,
	}
	m := NewSecrets(app.DefaultKeys(), store)
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
