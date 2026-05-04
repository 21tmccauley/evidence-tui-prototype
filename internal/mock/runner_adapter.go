package mock

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

const mockConcurrency = 4

// MockRunner is a scripted runner.Runner (lives in mock to avoid import cycles with catalog helpers).
type MockRunner struct {
	catalog map[runner.FetcherID]Fetcher
	states  map[runner.FetcherID]*mockState
	queue   []runner.FetcherID
	running int
}

type mockState struct {
	id      runner.FetcherID
	fetcher Fetcher
	script  Script
	beatIdx int
	runIdx  int
	status  runner.Status
}

func NewMockRunner(cat []Fetcher) *MockRunner {
	m := &MockRunner{
		catalog: map[runner.FetcherID]Fetcher{},
		states:  map[runner.FetcherID]*mockState{},
	}
	for _, f := range cat {
		m.catalog[f.ID] = f
	}
	return m
}

type beatMsg struct {
	ID    runner.FetcherID
	Index int
	Run   int
}

type finishTickMsg struct {
	ID  runner.FetcherID
	Run int
}

type cancelMsg struct{ ID runner.FetcherID }
type retryMsg struct{ ID runner.FetcherID }

func scheduleBeat(id runner.FetcherID, idx, run int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return beatMsg{ID: id, Index: idx, Run: run}
	})
}

func scheduleFinish(id runner.FetcherID, run int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return finishTickMsg{ID: id, Run: run}
	})
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func (m *MockRunner) Init() tea.Cmd { return nil }

func (m *MockRunner) Start(ids []runner.FetcherID) tea.Cmd {
	m.states = map[runner.FetcherID]*mockState{}
	m.queue = nil
	m.running = 0

	for _, id := range ids {
		f, ok := m.catalog[id]
		if !ok {
			continue
		}
		m.states[id] = &mockState{
			id:      id,
			fetcher: f,
			script:  Build(f),
			status:  runner.StatusQueued,
		}
		m.queue = append(m.queue, id)
	}
	return tea.Batch(m.fillRunning()...)
}

func (m *MockRunner) fillRunning() []tea.Cmd {
	cmds := []tea.Cmd{}
	for m.running < mockConcurrency && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		st, ok := m.states[id]
		if !ok {
			continue
		}
		st.status = runner.StatusRunning
		st.beatIdx = 0
		m.running++

		cmds = append(cmds, emit(runner.StartedMsg{ID: id}))
		if len(st.script.Beats) == 0 {
			cmds = append(cmds, scheduleFinish(id, st.runIdx, st.script.FinalDelay))
		} else {
			cmds = append(cmds, scheduleBeat(id, 0, st.runIdx, st.script.Beats[0].Delay))
		}
	}
	return cmds
}

func (m *MockRunner) Update(msg tea.Msg) (runner.Runner, tea.Cmd) {
	switch msg := msg.(type) {
	case beatMsg:
		st, ok := m.states[msg.ID]
		if !ok || msg.Run != st.runIdx || st.status != runner.StatusRunning {
			return m, nil
		}
		if msg.Index >= len(st.script.Beats) {
			return m, nil
		}
		beat := st.script.Beats[msg.Index]
		st.beatIdx = msg.Index + 1

		cmds := []tea.Cmd{
			emit(runner.OutputMsg{ID: msg.ID, Line: beat.Line}),
		}
		if st.beatIdx < len(st.script.Beats) {
			cmds = append(cmds, scheduleBeat(msg.ID, st.beatIdx, st.runIdx, st.script.Beats[st.beatIdx].Delay))
		} else {
			cmds = append(cmds, scheduleFinish(msg.ID, st.runIdx, st.script.FinalDelay))
		}
		return m, tea.Batch(cmds...)

	case finishTickMsg:
		st, ok := m.states[msg.ID]
		if !ok || msg.Run != st.runIdx || st.status != runner.StatusRunning {
			return m, nil
		}
		st.status = st.script.Final
		m.running--
		cmds := []tea.Cmd{
			emit(runner.FinishedMsg{
				ID:       msg.ID,
				Status:   st.script.Final,
				ExitCode: st.script.ExitCode,
			}),
		}
		cmds = append(cmds, m.fillRunning()...)
		return m, tea.Batch(cmds...)

	case cancelMsg:
		st, ok := m.states[msg.ID]
		if !ok {
			return m, nil
		}
		switch st.status {
		case runner.StatusRunning:
			st.runIdx++
			st.status = runner.StatusCancelled
			m.running--
			cmds := []tea.Cmd{
				emit(runner.FinishedMsg{ID: msg.ID, Status: runner.StatusCancelled}),
			}
			cmds = append(cmds, m.fillRunning()...)
			return m, tea.Batch(cmds...)
		case runner.StatusQueued:
			st.status = runner.StatusCancelled
			m.queue = removeID(m.queue, msg.ID)
			return m, emit(runner.FinishedMsg{ID: msg.ID, Status: runner.StatusCancelled})
		}
		return m, nil

	case retryMsg:
		st, ok := m.states[msg.ID]
		if !ok {
			return m, nil
		}
		if !st.status.Terminal() || st.status == runner.StatusOK {
			return m, nil
		}
		st.runIdx++
		st.status = runner.StatusQueued
		st.beatIdx = 0
		m.queue = append(m.queue, msg.ID)
		return m, tea.Batch(m.fillRunning()...)
	}
	return m, nil
}

func (m *MockRunner) Cancel(id runner.FetcherID) tea.Cmd {
	return emit(cancelMsg{ID: id})
}

func (m *MockRunner) Retry(id runner.FetcherID) tea.Cmd {
	return emit(retryMsg{ID: id})
}

func (m *MockRunner) Bind(_ runner.Sender) {}

func removeID(xs []runner.FetcherID, id runner.FetcherID) []runner.FetcherID {
	out := xs[:0]
	for _, x := range xs {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
