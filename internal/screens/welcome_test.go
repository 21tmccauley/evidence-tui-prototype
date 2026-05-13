package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/platforms"
)

func TestWelcomeEnterSendsSelectedProfileMsg(t *testing.T) {
	m := NewWelcome(app.DefaultKeys())

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a command")
	}
	if _, ok := cmd().(SelectedProfileMsg); !ok {
		t.Fatalf("expected SelectedProfileMsg from enter, got %T", cmd())
	}
}

func TestWelcomeSKeyOpensSecrets(t *testing.T) {
	m := NewWelcome(app.DefaultKeys())

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("s should produce a command")
	}
	if _, ok := cmd().(OpenSecretsMsg); !ok {
		t.Fatalf("expected OpenSecretsMsg from s, got %T", cmd())
	}
}

func TestWelcomeRendersDiscoverySummary(t *testing.T) {
	m := NewWelcomeWithOptions(app.DefaultKeys(), WelcomeOptions{
		Platforms: []platforms.Platform{
			{ID: "okta", DisplayName: "Okta", Fetchers: []platforms.Fetcher{
				{ID: "okta/users", Name: "Users"},
				{ID: "okta/apps", Name: "Apps"},
			}},
			{ID: "acme_widget", DisplayName: "Acme Widget", Fetchers: []platforms.Fetcher{
				{ID: "acme_widget/widget_check", Name: "Widget Check"},
			}},
		},
		EnvFilePath: "/tmp/test/.env",
	})
	m = m.Resize(140, 40)

	v := m.View()
	if !strings.Contains(v, "platforms") || !strings.Contains(v, "2") {
		t.Fatalf("expected platform count 2, got:\n%s", v)
	}
	if !strings.Contains(v, "fetchers") || !strings.Contains(v, "3") {
		t.Fatalf("expected fetcher count 3, got:\n%s", v)
	}
	if !strings.Contains(v, "/tmp/test/.env") {
		t.Fatalf("expected env file path in summary, got:\n%s", v)
	}
}

func TestWelcomeRendersNoAWSProfilePicker(t *testing.T) {
	// Regression: the legacy welcome showed AWS-specific profile cards
	// with paramify-prod / customer-acme defaults. The launchpad must not.
	m := NewWelcome(app.DefaultKeys())
	m = m.Resize(140, 40)
	v := m.View()
	for _, banned := range []string{"paramify-prod", "customer-acme", "select a profile", "sso login"} {
		if strings.Contains(v, banned) {
			t.Fatalf("Welcome must not show legacy AWS UI text %q, got:\n%s", banned, v)
		}
	}
}
