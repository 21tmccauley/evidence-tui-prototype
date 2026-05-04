package runner

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// FetcherID is a stable catalog id (e.g. EVD-…) carried in all runner messages.
type FetcherID string

func (id FetcherID) String() string { return string(id) }

// Status is a fetcher lifecycle state (DESIGN.md).
type Status int

const (
	StatusQueued Status = iota
	StatusRunning
	StatusOK
	StatusPartial
	StatusFailed
	StatusCancelled
)

func (s Status) String() string {
	switch s {
	case StatusQueued:
		return "queued"
	case StatusRunning:
		return "running"
	case StatusOK:
		return "ok"
	case StatusPartial:
		return "partial"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	}
	return "?"
}

func (s Status) Terminal() bool {
	return s == StatusOK || s == StatusPartial || s == StatusFailed || s == StatusCancelled
}

const StallThreshold = 4 * time.Second

// ScheduleStallTick is the Run screen's 1s stall cadence (shared constant).
func ScheduleStallTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return StallTickMsg{}
	})
}
