package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

// SessionLog is a concurrent-safe append-only support log (DESIGN.md Part 9: never log OutputMsg lines; use per-fetcher stdout/stderr logs for content).
type SessionLog struct {
	mu   sync.Mutex
	w    io.Writer
	file *os.File
	now  func() time.Time
}

// OpenSessionLog opens session-<ts>.log for append. On error, still returns a discard log so callers can defer Close(); surface the error to the user.
func OpenSessionLog(ts string) (*SessionLog, error) {
	if _, err := EnsureLogsDir(); err != nil {
		return newDiscardLog(), err
	}
	path, err := SessionLogPath(ts)
	if err != nil {
		return newDiscardLog(), err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return newDiscardLog(), fmt.Errorf("open session log %q: %w", path, err)
	}
	log := &SessionLog{w: f, file: f, now: time.Now}
	log.Logf("session opened: %s", filepath.Base(path))
	return log, nil
}

// NewWriter is a SessionLog backed by w (tests).
func NewWriter(w io.Writer) *SessionLog {
	return &SessionLog{w: w, now: time.Now}
}

func newDiscardLog() *SessionLog {
	return &SessionLog{w: io.Discard, now: time.Now}
}

// Logf appends one timestamped line (newline added if missing).
func (l *SessionLog) Logf(format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now().UTC().Format("2006-01-02T15:04:05.000Z")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.w, "%s %s\n", t, msg)
}

func (l *SessionLog) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.file.Sync()
	err := l.file.Close()
	l.file = nil
	l.w = io.Discard
	return err
}

// SenderTap logs StartedMsg/FinishedMsg to SessionLog then forwards to Inner (never logs OutputMsg).
type SenderTap struct {
	Inner runner.Sender
	Log   *SessionLog
}

func (t SenderTap) Send(msg tea.Msg) {
	switch m := msg.(type) {
	case runner.StartedMsg:
		t.Log.Logf("fetcher started: id=%s", m.ID)
	case runner.FinishedMsg:
		if m.ErrorReason == "" {
			t.Log.Logf("fetcher finished: id=%s status=%s exit=%d",
				m.ID, m.Status, m.ExitCode)
		} else {
			t.Log.Logf("fetcher finished: id=%s status=%s exit=%d reason=%q",
				m.ID, m.Status, m.ExitCode, m.ErrorReason)
		}
	}
	if t.Inner != nil {
		t.Inner.Send(msg)
	}
}
