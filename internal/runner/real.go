package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/evidence"
)

const cancelGrace = 5 * time.Second

// RealRunner invokes evidence-fetchers scripts via exec.CommandContext,
// honoring the contract in .cursor/rules/20-runner-contract.mdc.
type RealRunner struct {
	cfg    Config
	sender Sender

	mu           sync.Mutex
	states       map[FetcherID]*realState
	order        []FetcherID
	queue        []FetcherID
	running      int
	awsAuthOK    map[string]bool // memoized by profile+region once per Start() call
	pending      []FinishedMsg
	summaryWrote bool

	awsAuthMu sync.Mutex
}

type realState struct {
	id          FetcherID
	script      catalog.Script
	instance    Instance
	runIdx      int
	status      Status
	cancelFn    context.CancelFunc
	errorReason string
}

// NewReal builds a RealRunner; call Bind(program) once before Run for async messages.
func NewReal(cfg Config) *RealRunner {
	if cfg.AuthChecker == nil {
		cfg.AuthChecker = CLIAuthChecker{}
	}
	if cfg.MaxParallel < 1 {
		cfg.MaxParallel = 1
	}
	return &RealRunner{
		cfg:       cfg,
		states:    map[FetcherID]*realState{},
		awsAuthOK: map[string]bool{},
	}
}

func (r *RealRunner) Bind(s Sender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sender = s
}

func (r *RealRunner) ConfigureProfile(profile, region string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Profile = profile
	if region == "—" {
		region = ""
	}
	r.cfg.Region = region
}

func (r *RealRunner) Init() tea.Cmd { return nil }

func (r *RealRunner) Update(msg tea.Msg) (Runner, tea.Cmd) {
	if _, ok := msg.(TargetsMsg); !ok {
		return r, nil
	}
	return r, func() tea.Msg {
		r.flushPendingFailures()
		r.fillRunning()
		return nil
	}
}

func (r *RealRunner) Start(ids []FetcherID) tea.Cmd {
	evidenceErr := r.writeEvidenceSets(ids)

	r.mu.Lock()
	r.states = map[FetcherID]*realState{}
	r.order = nil
	r.queue = nil
	r.running = 0
	r.awsAuthOK = map[string]bool{}
	r.pending = nil
	r.summaryWrote = false

	targets := []Target{}
	instances := InstancesFromEnv(ids, r.cfg.Scripts, r.cfg.environment())
	for _, id := range ids {
		if _, ok := r.cfg.Scripts[id]; !ok {
			r.states[id] = &realState{id: id, status: StatusFailed}
			targets = append(targets, Target{ID: id, BaseID: id, Label: "missing"})
			r.pending = append(r.pending, FinishedMsg{
				ID: id, Status: StatusFailed,
				ErrorReason: fmt.Sprintf("fetcher %s not found in catalog", id),
			})
		}
	}
	if evidenceErr != nil {
		for _, id := range ids {
			if _, ok := r.cfg.Scripts[id]; !ok {
				continue
			}
			r.states[id] = &realState{id: id, status: StatusFailed}
			r.order = append(r.order, id)
			targets = append(targets, Target{ID: id, BaseID: id})
			r.pending = append(r.pending, FinishedMsg{
				ID:          id,
				Status:      StatusFailed,
				ErrorReason: fmt.Sprintf("write evidence_sets.json: %v", evidenceErr),
			})
		}
		r.mu.Unlock()
		return func() tea.Msg { return TargetsMsg{Targets: targets} }
	}
	for _, inst := range instances {
		s := r.cfg.Scripts[inst.BaseID]
		r.states[inst.ID] = &realState{
			id:       inst.ID,
			script:   s,
			instance: inst,
			status:   StatusQueued,
		}
		r.queue = append(r.queue, inst.ID)
		r.order = append(r.order, inst.ID)
		targets = append(targets, inst.Target())
	}
	r.mu.Unlock()

	return func() tea.Msg { return TargetsMsg{Targets: targets} }
}

func (r *RealRunner) writeEvidenceSets(ids []FetcherID) error {
	if r.cfg.OutputRoot == "" {
		return nil
	}
	selected := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := r.cfg.Scripts[id]; ok {
			selected = append(selected, string(id))
		}
	}
	if len(selected) == 0 {
		return nil
	}
	scripts := make(map[string]catalog.Script, len(r.cfg.Scripts))
	for id, s := range r.cfg.Scripts {
		scripts[string(id)] = s
	}
	return evidence.Write(
		selected,
		scripts,
		r.cfg.EvidenceSetsCompatPath,
		filepath.Join(r.cfg.OutputRoot, "evidence_sets.json"),
	)
}

// Cancel stops a running (SIGTERM then SIGKILL after cancelGrace) or queued fetcher.
func (r *RealRunner) Cancel(id FetcherID) tea.Cmd {
	return func() tea.Msg {
		r.mu.Lock()
		st, ok := r.states[id]
		if !ok {
			r.mu.Unlock()
			return nil
		}
		switch st.status {
		case StatusRunning:
			cancel := st.cancelFn
			r.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		case StatusQueued:
			st.status = StatusCancelled
			r.queue = removeFetcherID(r.queue, id)
			r.mu.Unlock()
			r.send(FinishedMsg{ID: id, Status: StatusCancelled})
		default:
			r.mu.Unlock()
		}
		return nil
	}
}

func (r *RealRunner) Retry(id FetcherID) tea.Cmd {
	return func() tea.Msg {
		r.mu.Lock()
		st, ok := r.states[id]
		if !ok || !st.status.Terminal() || st.status == StatusOK {
			r.mu.Unlock()
			return nil
		}
		st.runIdx++
		st.status = StatusQueued
		st.cancelFn = nil
		r.queue = append(r.queue, id)
		r.mu.Unlock()
		r.fillRunning()
		return nil
	}
}

func (r *RealRunner) fillRunning() {
	r.mu.Lock()
	type launch struct {
		id     FetcherID
		runIdx int
	}
	var launches []launch
	for r.running < r.cfg.MaxParallel && len(r.queue) > 0 {
		id := r.queue[0]
		r.queue = r.queue[1:]
		st, ok := r.states[id]
		if !ok {
			continue
		}
		st.status = StatusRunning
		r.running++
		launches = append(launches, launch{id: id, runIdx: st.runIdx})
	}
	r.mu.Unlock()

	for _, l := range launches {
		go r.execute(l.id, l.runIdx)
	}
}

func (r *RealRunner) flushPendingFailures() {
	r.mu.Lock()
	pending := append([]FinishedMsg(nil), r.pending...)
	r.pending = nil
	r.mu.Unlock()
	for _, msg := range pending {
		r.send(msg)
	}
}

func (r *RealRunner) execute(id FetcherID, runIdx int) {
	r.mu.Lock()
	st := r.states[id]
	if st == nil || st.runIdx != runIdx {
		r.mu.Unlock()
		return
	}
	script := st.script
	inst := st.instance
	r.mu.Unlock()

	runDir := OutputDirForInstance(r.cfg.OutputRoot, script.Key, inst)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		r.finish(id, runIdx, FinishedMsg{
			ID: id, Status: StatusFailed,
			ErrorReason: fmt.Sprintf("create output dir: %v", err),
		})
		return
	}

	var stdoutF, stderrF *os.File
	defer func() {
		syncCloseLogFile(stdoutF)
		syncCloseLogFile(stderrF)
	}()

	var err error
	stdoutF, err = os.Create(filepath.Join(runDir, "stdout.log"))
	if err != nil {
		r.finish(id, runIdx, FinishedMsg{
			ID: id, Status: StatusFailed,
			ErrorReason: fmt.Sprintf("open stdout.log: %v", err),
		})
		return
	}
	stderrF, err = os.Create(filepath.Join(runDir, "stderr.log"))
	if err != nil {
		r.finish(id, runIdx, FinishedMsg{
			ID: id, Status: StatusFailed,
			ErrorReason: fmt.Sprintf("open stderr.log: %v", err),
		})
		return
	}

	timeout := ResolveTimeout(script.Key)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	r.mu.Lock()
	if st := r.states[id]; st != nil && st.runIdx == runIdx {
		st.cancelFn = cancel
	}
	r.mu.Unlock()

	cmd := BuildInstanceCmd(ctx, r.cfg, script, inst)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = cancelGrace

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.finish(id, runIdx, FinishedMsg{
			ID: id, Status: StatusFailed,
			ErrorReason: fmt.Sprintf("stdout pipe: %v", err),
		})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.finish(id, runIdx, FinishedMsg{
			ID: id, Status: StatusFailed,
			ErrorReason: fmt.Sprintf("stderr pipe: %v", err),
		})
		return
	}
	if err := cmd.Start(); err != nil {
		r.finish(id, runIdx, FinishedMsg{
			ID: id, Status: StatusFailed,
			ErrorReason: fmt.Sprintf("start: %v", err),
		})
		return
	}

	r.sendIfFresh(id, runIdx, StartedMsg{ID: id})

	var wg sync.WaitGroup
	wg.Add(2)
	go r.pipeReader(&wg, id, runIdx, stdout, stdoutF)
	go r.pipeReader(&wg, id, runIdx, stderr, stderrF)
	wg.Wait()

	waitErr := cmd.Wait()

	syncCloseLogFile(stdoutF)
	syncCloseLogFile(stderrF)
	stdoutF, stderrF = nil, nil

	status, reason := r.classify(ctx, cmd, waitErr, script, inst, runDir, timeout)
	exit := -1
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	r.finish(id, runIdx, FinishedMsg{
		ID: id, Status: status, ExitCode: exit, ErrorReason: reason,
	})
}

func (r *RealRunner) pipeReader(wg *sync.WaitGroup, id FetcherID, runIdx int, src io.Reader, sink io.Writer) {
	defer wg.Done()
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(sink, line)
		r.sendIfFresh(id, runIdx, OutputMsg{ID: id, Line: line})
	}
}

// classify applies deadline, cancel, exit, then AWS post-flight (order matters).
func (r *RealRunner) classify(
	ctx context.Context,
	cmd *exec.Cmd,
	waitErr error,
	script catalog.Script,
	inst Instance,
	runDir string,
	timeout time.Duration,
) (Status, string) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return StatusFailed, fmt.Sprintf("timed out after %s", timeout)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return StatusCancelled, ""
	}
	if waitErr != nil {
		exit := -1
		if cmd.ProcessState != nil {
			exit = cmd.ProcessState.ExitCode()
		}
		tail := lastBytesOf(filepath.Join(runDir, "stderr.log"), 200)
		if tail == "" {
			return StatusFailed, fmt.Sprintf("exit %d", exit)
		}
		return StatusFailed, fmt.Sprintf("exit %d: %s", exit, tail)
	}
	return StatusOK, ""
}

// finish writes summary.json before FinishedMsg when this is the last finisher, then sendIfFresh and fillRunning (ordering avoids races and UI reordering).
func (r *RealRunner) finish(id FetcherID, runIdx int, msg FinishedMsg) {
	r.mu.Lock()
	if st := r.states[id]; st != nil && st.runIdx == runIdx {
		st.errorReason = msg.ErrorReason
		st.status = msg.Status
	}
	r.running--

	var (
		writeSummary bool
		outDir       string
		results      []SummaryResult
	)
	if !r.summaryWrote && r.running == 0 && len(r.queue) == 0 && r.allStatesTerminalLocked() {
		r.summaryWrote = true
		writeSummary = true
		outDir = r.cfg.OutputRoot
		results = r.collectSummaryResultsLocked()
	}
	r.mu.Unlock()

	if writeSummary {
		_ = SummaryWriter{}.WriteSummary(outDir, results)
	}

	r.sendIfFresh(id, runIdx, msg)
	r.fillRunning()
}

func (r *RealRunner) allStatesTerminalLocked() bool {
	for _, st := range r.states {
		if st == nil || !st.status.Terminal() {
			return false
		}
	}
	return true
}

func (r *RealRunner) collectSummaryResultsLocked() []SummaryResult {
	results := make([]SummaryResult, 0, len(r.order))
	for _, id := range r.order {
		st := r.states[id]
		if st == nil {
			continue
		}
		ok := st.status == StatusOK || st.status == StatusPartial
		checkName := st.script.Key
		if st.instance.Name != "" {
			checkName = checkName + "_" + st.instance.Name
		}
		results = append(results, SummaryResult{
			CheckName:   checkName,
			ScriptKey:   st.script.Key,
			Instance:    st.instance,
			Success:     ok,
			ErrorReason: strings.TrimSpace(st.errorReason),
		})
	}
	return results
}

func (r *RealRunner) sendIfFresh(id FetcherID, runIdx int, msg tea.Msg) {
	r.mu.Lock()
	stale := r.states[id] == nil || r.states[id].runIdx != runIdx
	sender := r.sender
	r.mu.Unlock()
	if stale || sender == nil {
		return
	}
	sender.Send(msg)
}

func (r *RealRunner) send(msg tea.Msg) {
	r.mu.Lock()
	sender := r.sender
	r.mu.Unlock()
	if sender == nil {
		return
	}
	sender.Send(msg)
}

// preflightOK memoizes CheckAWSAuth per profile+region; awsAuthMu serializes the first check.
func (r *RealRunner) preflightOK(profile, region string) bool {
	key := profile + "\x00" + region
	r.mu.Lock()
	if ok, found := r.awsAuthOK[key]; found {
		r.mu.Unlock()
		return ok
	}
	r.mu.Unlock()

	r.awsAuthMu.Lock()
	defer r.awsAuthMu.Unlock()

	r.mu.Lock()
	if ok, found := r.awsAuthOK[key]; found {
		r.mu.Unlock()
		return ok
	}
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), awsAuthTimeout)
	defer cancel()
	err := r.cfg.AuthChecker.CheckAWSAuth(ctx, profile, region)
	ok := err == nil

	r.mu.Lock()
	r.awsAuthOK[key] = ok
	r.mu.Unlock()
	return ok
}

func syncCloseLogFile(f *os.File) {
	if f == nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

func removeFetcherID(xs []FetcherID, id FetcherID) []FetcherID {
	out := xs[:0]
	for _, x := range xs {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func lastBytesOf(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return ""
	}
	size := stat.Size()
	off := int64(0)
	if size > int64(n) {
		off = size - int64(n)
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return ""
	}
	buf := make([]byte, n)
	read, _ := f.Read(buf)
	return string(buf[:read])
}
