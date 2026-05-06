package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

// captureSender is a goroutine-safe Sender that lets tests collect every
// message the runner emits and signal when a FinishedMsg arrives for the
// id we're watching.
type captureSender struct {
	mu      sync.Mutex
	msgs    []tea.Msg
	target  FetcherID
	doneCh  chan FinishedMsg
	doneSet bool
}

func newCaptureSender(target FetcherID) *captureSender {
	return &captureSender{
		target: target,
		doneCh: make(chan FinishedMsg, 1),
	}
}

func (s *captureSender) Send(msg tea.Msg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	if fm, ok := msg.(FinishedMsg); ok && fm.ID == s.target && !s.doneSet {
		s.doneSet = true
		s.doneCh <- fm
	}
	s.mu.Unlock()
}

func (s *captureSender) snapshot() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tea.Msg, len(s.msgs))
	copy(out, s.msgs)
	return out
}

func (s *captureSender) waitFinished(t *testing.T, d time.Duration) FinishedMsg {
	t.Helper()
	select {
	case fm := <-s.doneCh:
		return fm
	case <-time.After(d):
		t.Fatalf("timeout waiting for FinishedMsg{ID=%s}; saw: %v", s.target, s.snapshot())
		return FinishedMsg{}
	}
}

// repoRoot returns the absolute path to internal/runner/testdata/repo,
// where our fixture scripts live under fetchers/aws/.
func testdataRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, "testdata", "repo")
}

// requireBash skips the test when bash isn't on PATH (Windows CI, minimal
// containers). All integration tests in this file gate on it.
func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not on PATH: %v", err)
	}
}

// stubAuthChecker is a fake AuthChecker so tests don't need a real
// `aws` CLI. The Err field controls success/failure.
type stubAuthChecker struct {
	calls int
	err   error
}

func (s *stubAuthChecker) CheckAWSAuth(_ context.Context, _ string, _ string) error {
	s.calls++
	return s.err
}

func TestReal_PreflightCacheIsKeyedByProfileRegion(t *testing.T) {
	auth := &stubAuthChecker{}
	r := NewReal(Config{AuthChecker: auth})

	if !r.preflightOK("prod", "us-east-1") {
		t.Fatal("first preflight should pass")
	}
	if !r.preflightOK("prod", "us-east-1") {
		t.Fatal("cached preflight should pass")
	}
	if auth.calls != 1 {
		t.Fatalf("same profile/region should be cached, calls=%d", auth.calls)
	}
	if !r.preflightOK("prod", "us-west-2") {
		t.Fatal("second region preflight should pass")
	}
	if auth.calls != 2 {
		t.Fatalf("different region should not reuse cache, calls=%d", auth.calls)
	}
}

func TestReal_ConfigureProfileUpdatesConfig(t *testing.T) {
	r := NewReal(Config{Profile: "old", Region: "us-west-2"})
	r.ConfigureProfile("prod", "us-east-1")
	profile, region := EffectiveProfileRegion(r.cfg, Instance{})
	if profile != "prod" || region != "us-east-1" {
		t.Fatalf("profile/region: got %q/%q want prod/us-east-1", profile, region)
	}
	r.ConfigureProfile("prod", "—")
	_, region = EffectiveProfileRegion(r.cfg, Instance{})
	if region != "" {
		t.Fatalf("dash region should clear runner region, got %q", region)
	}
}

func startReal(t *testing.T, r *RealRunner, ids []FetcherID) {
	t.Helper()
	cmd := r.Start(ids)
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	_, next := r.Update(msg)
	if next != nil {
		next()
	}
}

func TestReal_OK(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-OK"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				Name:       "Test OK",
				ScriptFile: "fetchers/aws/echo_ok.sh",
				Source:     "test", // non-aws: skip pre-flight and post-flight
				Key:        "echo_ok",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusOK {
		t.Errorf("status: got %s, want ok; reason=%q", fm.Status, fm.ErrorReason)
	}
	if fm.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", fm.ExitCode)
	}
	if fm.ErrorReason != "" {
		t.Errorf("ok run should have empty ErrorReason, got %q", fm.ErrorReason)
	}

	// Lifecycle: at least one StartedMsg and the two known stdout lines.
	saw := sender.snapshot()
	var seenStarted bool
	var stdoutLines []string
	for _, m := range saw {
		switch v := m.(type) {
		case StartedMsg:
			seenStarted = true
		case OutputMsg:
			stdoutLines = append(stdoutLines, v.Line)
		}
	}
	if !seenStarted {
		t.Error("expected a StartedMsg before FinishedMsg")
	}
	if !containsString(stdoutLines, "ok-line-1") || !containsString(stdoutLines, "ok-line-2") {
		t.Errorf("expected ok-line-1/2 in OutputMsgs, got %v", stdoutLines)
	}

	// Log files written.
	stdoutLog := readFile(t, filepath.Join(outRoot, "echo_ok", "stdout.log"))
	if !strings.Contains(stdoutLog, "ok-line-1") || !strings.Contains(stdoutLog, "ok-line-2") {
		t.Errorf("stdout.log should tee both lines, got: %q", stdoutLog)
	}
	stderrLog := readFile(t, filepath.Join(outRoot, "echo_ok", "stderr.log"))
	if !strings.Contains(stderrLog, "stderr-line-1") {
		t.Errorf("stderr.log should contain the stderr line, got: %q", stderrLog)
	}
}

func TestReal_WritesEvidenceSetsCompatibilityAndAuditCopies(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()
	compatPath := filepath.Join(t.TempDir(), "repo", "evidence_sets.json")

	const id FetcherID = "EVD-TEST-EVIDENCE-SETS"
	cfg := Config{
		FetcherRepoRoot:        repoRoot,
		OutputRoot:             outRoot,
		EvidenceSetsCompatPath: compatPath,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:           string(id),
				Name:         "Evidence Sets Test",
				Description:  "Checks evidence_sets rendering",
				ScriptFile:   "fetchers/aws/echo_ok.sh",
				Source:       "test",
				Key:          "echo_ok",
				Instructions: "Script: echo_ok.sh. Commands executed: aws sts get-caller-identity",
				ValidationRules: []catalog.ValidationRule{
					{Regex: `"ok":\s*true`},
				},
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusOK {
		t.Fatalf("status: got %s, want ok; reason=%q", fm.Status, fm.ErrorReason)
	}

	for _, path := range []string{compatPath, filepath.Join(outRoot, "evidence_sets.json")} {
		body := readFile(t, path)
		if !strings.Contains(body, `"evidence_sets"`) {
			t.Fatalf("%s missing evidence_sets root:\n%s", path, body)
		}
		if !strings.Contains(body, `"script_file": "fetchers/aws/echo_ok.sh"`) {
			t.Fatalf("%s missing script_file:\n%s", path, body)
		}
		if !strings.Contains(body, `"instructions": [`) {
			t.Fatalf("%s missing rich-text instructions:\n%s", path, body)
		}
		if !strings.Contains(body, `"validationRules": [`) {
			t.Fatalf("%s missing validationRules:\n%s", path, body)
		}
	}
}

func TestReal_NonZeroExit(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-FAIL"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/echo_fail.sh",
				Source:     "test",
				Key:        "echo_fail",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusFailed {
		t.Errorf("status: got %s, want failed", fm.Status)
	}
	if fm.ExitCode != 7 {
		t.Errorf("exit code: got %d, want 7", fm.ExitCode)
	}
	if !strings.Contains(fm.ErrorReason, "boom") {
		t.Errorf("ErrorReason should include the stderr tail, got: %q", fm.ErrorReason)
	}
	if !strings.Contains(fm.ErrorReason, "exit 7") {
		t.Errorf("ErrorReason should include exit code, got: %q", fm.ErrorReason)
	}
}

func TestReal_ForwardsAllOutputLines_NoUICapInRunner(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-SPAM"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/spam_600_lines.sh",
				Source:     "test",
				Key:        "spam_600_lines",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusOK {
		t.Errorf("status: got %s, want ok; reason=%q", fm.Status, fm.ErrorReason)
	}

	saw := sender.snapshot()
	var lines []string
	for _, m := range saw {
		if om, ok := m.(OutputMsg); ok && om.ID == id {
			lines = append(lines, om.Line)
		}
	}
	if got, want := len(lines), 600; got != want {
		t.Fatalf("runner must forward all output lines (UI cap is in screen), got %d want %d", got, want)
	}

	stdoutLog := readFile(t, filepath.Join(outRoot, "spam_600_lines", "stdout.log"))
	if !strings.Contains(stdoutLog, "line-600") {
		t.Errorf("stdout.log should contain the last line, got tail: %q", stdoutLog[max(0, len(stdoutLog)-200):])
	}
}

// AWS post-flight: a script that exits 0 but writes evidence with
// `metadata.account_id == "unknown"` must be downgraded to Failed with
// the post-flight error_reason. Mirrors run_fetchers.py:518-526.
func TestReal_AWSPostflightDowngrade(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-UNKNOWN"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/echo_unknown_identity.sh",
				Source:     "aws",
				Key:        "echo_unknown_identity",
			},
		},
		AuthChecker: &stubAuthChecker{}, // succeed, so we exercise post-flight
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusFailed {
		t.Errorf("status: got %s, want failed (post-flight downgrade); reason=%q", fm.Status, fm.ErrorReason)
	}
	if !strings.Contains(fm.ErrorReason, "unknown AWS identity") {
		t.Errorf("ErrorReason should mention unknown AWS identity, got: %q", fm.ErrorReason)
	}
}

// AWS pre-flight: when the AuthChecker fails, AWS-source fetchers must
// fail fast with the SSO-login hint. Mirrors run_fetchers.py:474-482.
func TestReal_AWSPreflightFails(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-PRE"
	checker := &stubAuthChecker{err: errors.New("not signed in")}
	cfg := Config{
		Profile:         "my-profile",
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/echo_ok.sh", // would succeed; we never run it
				Source:     "aws",
				Key:        "echo_ok",
			},
		},
		AuthChecker: checker,
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 5*time.Second)
	if fm.Status != StatusFailed {
		t.Errorf("status: got %s, want failed", fm.Status)
	}
	if !strings.Contains(fm.ErrorReason, "AWS authentication missing") {
		t.Errorf("ErrorReason should mention AWS auth, got: %q", fm.ErrorReason)
	}
	if !strings.Contains(fm.ErrorReason, "my-profile") {
		t.Errorf("ErrorReason should include the profile name, got: %q", fm.ErrorReason)
	}
	if checker.calls != 1 {
		t.Errorf("AuthChecker should have been called once, got %d", checker.calls)
	}

	// Verify the script never ran (no stdout.log file written).
	stdoutPath := filepath.Join(outRoot, "echo_ok", "stdout.log")
	if _, err := os.Stat(stdoutPath); err == nil {
		t.Errorf("script must not run when pre-flight fails, but stdout.log exists at %s", stdoutPath)
	}
}

// Non-AWS catalog sources do not run AWS auth preflight.
func TestReal_NonAWSSourceSkipsAWSAuthChecker(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-KNOWBE4"
	checker := &stubAuthChecker{}
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Environ:         nil,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/echo_ok.sh",
				Source:     "knowbe4",
				Key:        "echo_ok",
			},
		},
		AuthChecker: checker,
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusOK {
		t.Errorf("status: got %s, want ok; reason=%q", fm.Status, fm.ErrorReason)
	}
	if checker.calls != 0 {
		t.Errorf("non-AWS fetcher should not run AWS auth check, got %d calls", checker.calls)
	}
}

// Pre-flight is memoized: two AWS-source fetchers share one AuthChecker
// call.
func TestReal_AWSPreflightMemoized(t *testing.T) {
	requireBash(t)
	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	checker := &stubAuthChecker{}
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			// Different keys => separate output dirs (same key shares log files).
			"a": {ID: "EVD-A", ScriptFile: "fetchers/aws/echo_ok.sh", Source: "aws", Key: "echo_ok"},
			"b": {ID: "EVD-B", ScriptFile: "fetchers/aws/echo_ok_alt.sh", Source: "aws", Key: "echo_ok_alt"},
		},
		AuthChecker: checker,
	}
	r := NewReal(cfg)
	doneA := newCaptureSender("a")
	doneB := newCaptureSender("b")
	// One sender that signals on both ids.
	combined := &dualSender{a: doneA, b: doneB}
	r.Bind(combined)
	startReal(t, r, []FetcherID{"a", "b"})

	doneA.waitFinished(t, 10*time.Second)
	doneB.waitFinished(t, 10*time.Second)

	if checker.calls != 1 {
		t.Errorf("AuthChecker should be called exactly once for two AWS fetchers in one run, got %d", checker.calls)
	}
}

type dualSender struct{ a, b *captureSender }

func (d *dualSender) Send(msg tea.Msg) {
	d.a.Send(msg)
	d.b.Send(msg)
}

func TestReal_TimeoutEnforced(t *testing.T) {
	requireBash(t)
	t.Setenv("FETCHER_TIMEOUT", "1")

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-TIMEOUT"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/sleep_timeout.sh",
				Source:     "test",
				Key:        "sleep_timeout",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 10*time.Second)
	if fm.Status != StatusFailed {
		t.Errorf("status: got %s, want failed; reason=%q", fm.Status, fm.ErrorReason)
	}
	if !strings.Contains(fm.ErrorReason, "timed out") {
		t.Errorf("ErrorReason should mention timeout, got %q", fm.ErrorReason)
	}

	stdoutLog := readFile(t, filepath.Join(outRoot, "sleep_timeout", "stdout.log"))
	if !strings.Contains(stdoutLog, "before-sleep") {
		t.Errorf("stdout.log should contain before-sleep, got %q", stdoutLog)
	}
	if strings.Contains(stdoutLog, "after-sleep") {
		t.Errorf("stdout.log must not contain after-sleep (script should be killed), got %q", stdoutLog)
	}
}

func TestReal_CancelHonorsSIGTERM(t *testing.T) {
	requireBash(t)

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-CANCEL-TERM"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/loop_term_exits.sh",
				Source:     "test",
				Key:        "loop_term_exits",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	// Give the process a moment to start.
	time.Sleep(150 * time.Millisecond)
	_ = r.Cancel(id)()

	fm := sender.waitFinished(t, 5*time.Second)
	if fm.Status != StatusCancelled {
		t.Errorf("status: got %s, want cancelled; reason=%q", fm.Status, fm.ErrorReason)
	}
}

func TestReal_CancelEscalatesToSIGKILL(t *testing.T) {
	requireBash(t)

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-CANCEL-KILL"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/loop_term_ignored.sh",
				Source:     "test",
				Key:        "loop_term_ignored",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	time.Sleep(150 * time.Millisecond)
	start := time.Now()
	_ = r.Cancel(id)()

	fm := sender.waitFinished(t, 12*time.Second)
	elapsed := time.Since(start)
	if fm.Status != StatusCancelled {
		t.Errorf("status: got %s, want cancelled; reason=%q", fm.Status, fm.ErrorReason)
	}
	// We shouldn't hang indefinitely if SIGTERM is ignored; allow a generous
	// budget since CI can be slow.
	if elapsed > 11*time.Second {
		t.Errorf("cancel escalation took too long: %s", elapsed)
	}
}

func TestReal_RetryDropsStaleOutputAfterCancel(t *testing.T) {
	requireBash(t)

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-RETRY-STALE"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts: map[FetcherID]catalog.Script{
			id: {
				ID:         string(id),
				ScriptFile: "fetchers/aws/cancel_then_chatty.sh",
				Source:     "test",
				Key:        "cancel_then_chatty",
			},
		},
		AuthChecker: &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	// Cancel immediately so we (reliably) take the TERM trap path.
	_ = r.Cancel(id)()
	// Immediately retry so any late output from the canceled attempt becomes stale.
	_ = r.Retry(id)()

	// Wait for the retry attempt to finish (it exits quickly on the non-cancel path).
	_ = sender.waitFinished(t, 5*time.Second)

	saw := sender.snapshot()
	for _, m := range saw {
		if om, ok := m.(OutputMsg); ok && om.ID == id && om.Line == "late-after-term" {
			t.Fatalf("stale output must be dropped after Retry, but saw %q in OutputMsgs", om.Line)
		}
	}
}

func TestReal_UnknownIDFailsCleanly(t *testing.T) {
	requireBash(t)

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	const id FetcherID = "EVD-TEST-UNKNOWN-ID"
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts:         map[FetcherID]catalog.Script{},
		AuthChecker:     &stubAuthChecker{},
	}
	r := NewReal(cfg)
	sender := newCaptureSender(id)
	r.Bind(sender)
	startReal(t, r, []FetcherID{id})

	fm := sender.waitFinished(t, 5*time.Second)
	if fm.Status != StatusFailed {
		t.Errorf("status: got %s, want failed", fm.Status)
	}
	if !strings.Contains(fm.ErrorReason, "not found in catalog") {
		t.Errorf("ErrorReason should mention missing catalog entry, got %q", fm.ErrorReason)
	}
}

func TestReal_ConcurrencyCapIsRespected(t *testing.T) {
	requireBash(t)

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	// 6 fetchers that never exit unless cancelled: we just want to observe
	// StartedMsg emission and ensure only 4 start immediately.
	scripts := map[FetcherID]catalog.Script{}
	ids := make([]FetcherID, 0, 6)
	for i := 0; i < 6; i++ {
		id := FetcherID(fmt.Sprintf("EVD-TEST-CONC-%d", i))
		ids = append(ids, id)
		scripts[id] = catalog.Script{
			ID:         string(id),
			ScriptFile: "fetchers/aws/loop_term_exits.sh",
			Source:     "test",
			Key:        fmt.Sprintf("conc_%d", i),
		}
	}
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts:         scripts,
		AuthChecker:     &stubAuthChecker{},
		MaxParallel:     4,
	}
	r := NewReal(cfg)

	// Sender that counts StartedMsg occurrences.
	started := &startedCounter{}
	r.Bind(started)
	startReal(t, r, ids)

	time.Sleep(250 * time.Millisecond)
	if got := started.count(); got > 4 {
		t.Fatalf("expected at most 4 started within concurrency cap, got %d", got)
	}
	// Cancel anything running/queued so the goroutines drain quickly.
	for _, id := range ids {
		_ = r.Cancel(id)()
	}
}

func TestReal_DefaultMaxParallelIsOne(t *testing.T) {
	requireBash(t)

	repoRoot := testdataRepoRoot(t)
	outRoot := t.TempDir()

	scripts := map[FetcherID]catalog.Script{}
	ids := make([]FetcherID, 0, 3)
	for i := 0; i < 3; i++ {
		id := FetcherID(fmt.Sprintf("EVD-TEST-DEFPAR-%d", i))
		ids = append(ids, id)
		scripts[id] = catalog.Script{
			ID:         string(id),
			ScriptFile: "fetchers/aws/loop_term_exits.sh",
			Source:     "test",
			Key:        fmt.Sprintf("defpar_%d", i),
		}
	}
	cfg := Config{
		FetcherRepoRoot: repoRoot,
		OutputRoot:      outRoot,
		Scripts:         scripts,
		AuthChecker:     &stubAuthChecker{},
		// MaxParallel omitted: zero should normalize to 1 in NewReal.
	}
	r := NewReal(cfg)

	started := &startedCounter{}
	r.Bind(started)
	startReal(t, r, ids)

	time.Sleep(250 * time.Millisecond)
	if got := started.count(); got != 1 {
		t.Fatalf("with default parallelism want exactly 1 started quickly, got %d", got)
	}
	for _, id := range ids {
		_ = r.Cancel(id)()
	}
}

type startedCounter struct {
	mu    sync.Mutex
	start int
}

func (s *startedCounter) Send(msg tea.Msg) {
	if _, ok := msg.(StartedMsg); !ok {
		return
	}
	s.mu.Lock()
	s.start++
	s.mu.Unlock()
}

func (s *startedCounter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.start
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
