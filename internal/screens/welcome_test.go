package screens

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/preflight"
)

type welcomeChecker struct {
	calls int
	err   error
}

func (w *welcomeChecker) CheckAWSAuth(context.Context, string, string) error {
	w.calls++
	return w.err
}

type welcomeLogin struct {
	calls int
	err   error
}

func (w *welcomeLogin) LoginAWS(context.Context, string) error {
	w.calls++
	return w.err
}

func TestWelcomeChecksCredentialsBeforeSelection(t *testing.T) {
	checker := &welcomeChecker{}
	m := NewWelcomeWithOptions(app.DefaultKeys(), WelcomeOptions{
		Profiles: []Profile{{Name: "prod", Region: "us-east-1"}},
		Credential: &preflight.Service{
			Checker: checker,
		},
	})

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should start credential check")
	}
	checkMsg := cmd()
	m, cmd = m.Update(checkMsg)
	if checker.calls != 1 {
		t.Fatalf("credential checker calls: got %d want 1", checker.calls)
	}
	selected, ok := cmd().(SelectedProfileMsg)
	if !ok {
		t.Fatalf("expected SelectedProfileMsg after successful check")
	}
	if selected.Profile.Name != "prod" {
		t.Fatalf("selected profile: got %#v", selected.Profile)
	}
}

func TestWelcomeSSOLoginRetriesCredentialCheck(t *testing.T) {
	checker := &welcomeChecker{err: errors.New("aws sts: SSO session has expired")}
	login := &welcomeLogin{}
	m := NewWelcomeWithOptions(app.DefaultKeys(), WelcomeOptions{
		Profiles: []Profile{{Name: "prod", Region: "us-east-1"}},
		Credential: &preflight.Service{
			Checker: checker,
			Login:   login,
		},
	})

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd = m.Update(cmd())
	if cmd != nil {
		t.Fatal("failed credential check should not advance")
	}
	if !m.ssoReady {
		t.Fatal("SSO failure should enable login action")
	}

	checker.err = nil
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("o should start SSO login")
	}
	m, cmd = m.Update(cmd())
	if login.calls != 1 {
		t.Fatalf("login calls: got %d want 1", login.calls)
	}
	if cmd == nil {
		t.Fatal("successful login should retry credential check")
	}
	m, cmd = m.Update(cmd())
	if checker.calls != 2 {
		t.Fatalf("credential checker calls after retry: got %d want 2", checker.calls)
	}
	if _, ok := cmd().(SelectedProfileMsg); !ok {
		t.Fatalf("expected SelectedProfileMsg after successful retry")
	}
}
