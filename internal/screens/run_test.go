package screens

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/app"
	"github.com/paramify/evidence-tui-prototype/internal/mock"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

func TestRunScreen_CapsOutputTailTo400Lines(t *testing.T) {
	cat := mock.Catalog()
	if len(cat) == 0 {
		t.Fatal("mock catalog unexpectedly empty")
	}
	id := cat[0].ID

	// The output cap logic lives entirely in Update(OutputMsg), so a dummy
	// runner is fine; we never call Init/Start here.
	var r runner.Runner = &noopRunner{}

	m := NewRun(app.DefaultKeys(), "", []runner.FetcherID{id}, r)

	// Feed 500 output lines.
	for i := 1; i <= 500; i++ {
		line := fmt.Sprintf("line-%d", i)
		var cmd any
		m, cmd = m.Update(runner.OutputMsg{ID: id, Line: line})
		_ = cmd
	}

	st := m.states[id]
	if st == nil {
		t.Fatalf("missing state for id %s", id)
	}
	if got, want := len(st.output), 400; got != want {
		t.Fatalf("output tail length: got %d want %d", got, want)
	}
	if got, want := st.output[0], "line-101"; got != want {
		t.Fatalf("first line after cap: got %q want %q", got, want)
	}
	if got, want := st.output[len(st.output)-1], "line-500"; got != want {
		t.Fatalf("last line after cap: got %q want %q", got, want)
	}
}

// noopRunner satisfies runner.Runner for tests that only exercise screen state
// transitions without starting any execution.
type noopRunner struct{}

func (n *noopRunner) Init() tea.Cmd { return nil }
func (n *noopRunner) Update(tea.Msg) (runner.Runner, tea.Cmd) {
	return n, nil
}
func (n *noopRunner) Start([]runner.FetcherID) tea.Cmd { return nil }
func (n *noopRunner) Cancel(runner.FetcherID) tea.Cmd  { return nil }
func (n *noopRunner) Retry(runner.FetcherID) tea.Cmd   { return nil }
func (n *noopRunner) Bind(runner.Sender)               {}
