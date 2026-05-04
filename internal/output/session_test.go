package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

func TestSessionLog_Logf_WritesTimestampedLine(t *testing.T) {
	var buf bytes.Buffer
	log := NewWriter(&buf)
	log.now = func() time.Time { return time.Date(2026, 5, 4, 9, 0, 0, 123_000_000, time.UTC) }

	log.Logf("hello %s", "world")

	got := buf.String()
	want := "2026-05-04T09:00:00.123Z hello world\n"
	if got != want {
		t.Fatalf("Logf: got %q want %q", got, want)
	}
}

func TestSessionLog_Logf_IsConcurrencySafe(t *testing.T) {
	var buf bytes.Buffer
	log := NewWriter(&buf)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			log.Logf("event-%d", i)
		}(i)
	}
	wg.Wait()

	// 50 distinct lines, all newline-terminated.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("expected 50 lines, got %d (output corruption suggests a missing mutex)", len(lines))
	}
}

func TestOpenSessionLog_CreatesFileUnderHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PARAMIFY_FETCHER_HOME", tmp)

	ts := "2026-05-04T09-00-00Z"
	log, err := OpenSessionLog(ts)
	if err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	defer log.Close()

	log.Logf("hello")
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(tmp, "logs", "session-"+ts+".log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(b), "session opened") {
		t.Fatalf("expected 'session opened' header, got: %q", string(b))
	}
	if !strings.Contains(string(b), "hello") {
		t.Fatalf("expected logged message, got: %q", string(b))
	}
}

func TestSessionLog_NilSafe(t *testing.T) {
	var l *SessionLog
	l.Logf("doesn't crash")
	if err := l.Close(); err != nil {
		t.Fatalf("nil.Close: %v", err)
	}
}

// captureSender records every msg pushed through it. Mirrors the helper in
// internal/runner/real_test.go.
type captureSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *captureSender) Send(msg tea.Msg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	s.mu.Unlock()
}

func TestSenderTap_LogsLifecycleMessages_AndForwards(t *testing.T) {
	var buf bytes.Buffer
	log := NewWriter(&buf)
	inner := &captureSender{}
	tap := SenderTap{Inner: inner, Log: log}

	tap.Send(runner.StartedMsg{ID: "EVD-FOO"})
	tap.Send(runner.OutputMsg{ID: "EVD-FOO", Line: "secret-account-id-12345"})
	tap.Send(runner.FinishedMsg{ID: "EVD-FOO", Status: runner.StatusOK, ExitCode: 0})
	tap.Send(runner.FinishedMsg{ID: "EVD-BAR", Status: runner.StatusFailed, ExitCode: 7, ErrorReason: "exit 7: boom"})

	// All messages forwarded.
	if got := len(inner.msgs); got != 4 {
		t.Fatalf("forwarded msgs: got %d want 4", got)
	}

	out := buf.String()
	if !strings.Contains(out, "fetcher started: id=EVD-FOO") {
		t.Errorf("missing started log: %q", out)
	}
	if !strings.Contains(out, "fetcher finished: id=EVD-FOO status=ok exit=0") {
		t.Errorf("missing ok finished log: %q", out)
	}
	if !strings.Contains(out, "fetcher finished: id=EVD-BAR status=failed exit=7 reason=") ||
		!strings.Contains(out, `"exit 7: boom"`) {
		t.Errorf("missing failed finished log with reason: %q", out)
	}

	// CRITICAL: OutputMsg lines must NOT appear in the session log
	// (they may carry secret-bearing content).
	if strings.Contains(out, "secret-account-id-12345") {
		t.Fatalf("session log leaked OutputMsg content: %q", out)
	}
}

func TestSenderTap_NilInner_DoesNotPanic(t *testing.T) {
	var buf bytes.Buffer
	tap := SenderTap{Inner: nil, Log: NewWriter(&buf)}
	tap.Send(runner.StartedMsg{ID: "EVD-X"})
	if !strings.Contains(buf.String(), "EVD-X") {
		t.Fatalf("expected log entry even with nil inner, got: %q", buf.String())
	}
}
