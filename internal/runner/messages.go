package runner

// Messages for the Run screen from a Runner (private runner messages stay inside the runner's Update).

// StartedMsg fires when a queued fetcher transitions to running.
type StartedMsg struct {
	ID FetcherID
}

// OutputMsg carries one captured stdout/stderr line (or, in mock mode, one
// scripted beat).
type OutputMsg struct {
	ID   FetcherID
	Line string
}

// Target describes one card the Run screen should render. Real runs may expand
// a selected base fetcher into multiple instance targets.
type Target struct {
	ID     FetcherID
	BaseID FetcherID
	Label  string
}

// TargetsMsg replaces the Run screen's initial selected fetcher IDs with the
// concrete execution targets the runner will emit messages for.
type TargetsMsg struct {
	Targets []Target
}

// FinishedMsg is the terminal outcome (ErrorReason matches summary.json; see DESIGN.md Part 3 §11).
type FinishedMsg struct {
	ID          FetcherID
	Status      Status
	ExitCode    int
	ErrorReason string
}

// StallTickMsg is emitted by the Run screen's stall ticker.
type StallTickMsg struct{}
