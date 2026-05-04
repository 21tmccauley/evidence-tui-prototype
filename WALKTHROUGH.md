# How paramify-fetcher Works — A Code Walkthrough

> Audience: Go developers somewhere between "I've written hello world" and
> "I've shipped a small service." If you've used another language with classes
> and event loops, you'll feel at home; this doc points out where Go is the
> same and where it surprises you. No prior Bubble Tea experience required.
>
> How to read it: linearly the first time, then jump around. Every concept is
> grounded in actual code from this project — file paths and line numbers
> point at where to keep reading on your own.

---

## Table of contents

1. [The 30,000-foot view](#part-1--the-30000-foot-view)
2. [Bubble Tea primer (the Elm Architecture in Go)](#part-2--bubble-tea-primer)
3. [Project layout](#part-3--project-layout)
4. [Go fundamentals you'll see](#part-4--go-fundamentals-youll-see)
5. [The Runner interface — the heart of the architecture](#part-5--the-runner-interface)
6. [The Mock runner — scripted, deterministic, no goroutines](#part-6--the-mock-runner)
7. [The Real runner — subprocesses, pipes, goroutines](#part-7--the-real-runner)
8. [The Catalog — JSON, `//go:embed`, custom unmarshal](#part-8--the-catalog)
9. [The screens — Welcome, Select, Run, Review](#part-9--the-screens)
10. [Wiring — root model and `main.go`](#part-10--wiring)
11. [Why we did it this way](#part-11--why-we-did-it-this-way)
12. [Reading order — where to start for common tasks](#part-12--reading-order)

---

## Part 1 — The 30,000-foot view

`paramify-fetcher` is a terminal UI ("TUI") that walks an operator through
collecting compliance evidence from cloud platforms, then uploading it to
Paramify. The actual data collection is done by ~70 bash and Python scripts
that already exist in a sibling repo (`evidence-fetchers/`). Our job is the
*UI* and the *orchestration*: pick which scripts to run, run them in
parallel, stream their output, classify their results, package the artifacts.

The program is built around four screens:

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Welcome  ───────►  Select  ───────►  Run  ───────►  Review  ───►  exit  │
│  pick a profile    pick fetchers     stream output   summary +           │
│                                                      mock upload         │
└──────────────────────────────────────────────────────────────────────────┘
```

There are two "runners" — concrete implementations of one interface — that
turn fetcher selections into running work:

- **Mock runner**: produces deterministic, scripted output ("ok: 12
  records"). No subprocess, no AWS, no network. Powers the demo, the tests,
  and screenshots.
- **Real runner**: invokes actual scripts via `os/exec`, captures
  stdout/stderr line-by-line, runs an AWS pre-flight check, classifies
  results, writes log files. The thing that ships.

The whole thing is one statically-linked Go binary with the catalog of
fetchers (`evidence_fetchers_catalog.json`) baked in via `//go:embed`. No
runtime dependencies, no Python, no `pip install`.

---

## Part 2 — Bubble Tea primer

[Bubble Tea](https://github.com/charmbracelet/bubbletea) is the TUI
framework we use. It's a Go port of [The Elm
Architecture](https://guide.elm-lang.org/architecture/), which is a fancy
name for a very simple idea:

```
Msg ─────►  Update(msg, model) ─────►  (newModel, Cmd)
                                            │
                          ┌─────────────────┘
                          ▼
                       View(model) ─────► what the user sees
                          │
                          ▼
                  user presses a key, terminal resizes,
                  timer fires, etc.  ───────►  new Msg
```

Three pieces of vocabulary:

- **Model** — the entire state of your program, packaged into one Go struct
  (or one struct that contains other structs). It's *data*. No methods that
  hide state, no global variables.

- **Msg** — anything that happens. Key presses, mouse events, window
  resizes, timer ticks, "the user clicked save", "the network call
  finished" — they're all just values. Any type can be a `tea.Msg` because
  `tea.Msg` is `interface{}` (now `any`).

- **Cmd** — a function that produces a future Msg. It's how you do
  side-effects. You don't open a file in `Update`; you return a `tea.Cmd`
  that opens the file and produces a `FileOpenedMsg` when it's done.

Bubble Tea's loop is roughly:

```go
for {
    msg := <-events                    // a key press, a timer, …
    model, cmd := model.Update(msg)
    render(model.View())
    go runCmd(cmd, sendBackToEvents)   // side-effects happen here
}
```

The big idea: **`Update` is pure**. Given the same model and the same msg,
it always returns the same new model. No goroutines spawned in `Update`,
no file I/O, no `time.Sleep`. If you need any of those, you return a `Cmd`
and Bubble Tea runs it for you on a separate goroutine. The result of the
Cmd comes back as another `Msg` on the next iteration.

This is *the* mental model for the whole codebase. If you remember nothing
else, remember:

> Update doesn't *do* things. It *describes* what to do, by returning
> Cmds. Bubble Tea executes them.

You'll see this pattern everywhere — for example, the run screen never
calls `r.Cancel(id)` directly:

```go
// screens/run.go
case key.Matches(msg, m.keys.Cancel):
    id := m.focusedID()
    if st, ok := m.states[id]; ok && !st.status.Terminal() {
        cmds = append(cmds, m.runner.Cancel(id))   // ← describes the cancel
    }
```

`m.runner.Cancel(id)` *returns* a `tea.Cmd`. The actual signal sending
happens later, in another goroutine, when Bubble Tea runs that command.

### The Model interface

```go
type Model interface {
    Init() Cmd                                   // any startup work
    Update(msg Msg) (Model, Cmd)                 // state transition
    View() string                                // how to render
}
```

Three methods. We have one for each screen and one for the root that owns
all of them. That's it.

---

## Part 3 — Project layout

```
evidence-tui-prototype/
├── main.go                              ← program entrypoint, flag parsing
├── go.mod / go.sum                      ← Go module metadata
├── DESIGN.md                            ← product design doc
├── WALKTHROUGH.md                       ← this file
└── internal/
    ├── app/
    │   ├── keys.go                      ← global key bindings
    │   └── styles.go                    ← Lipgloss theme (colors, borders)
    ├── components/
    │   ├── header.go                    ← persistent top bar
    │   └── footer.go                    ← persistent bottom bar (key hints)
    ├── catalog/
    │   ├── schema.go                    ← Go structs mirroring the JSON
    │   ├── loader.go                    ← parsing + validation
    │   ├── embedded.go                  ← //go:embed of the catalog
    │   └── embedded/
    │       └── evidence_fetchers_catalog.json   ← baked into the binary
    ├── runner/
    │   ├── runner.go                    ← THE interface
    │   ├── messages.go                  ← public message types
    │   ├── types.go                     ← FetcherID, Status, helpers
    │   ├── sender.go                    ← Sender interface (goroutine bridge)
    │   ├── exec.go                      ← exec.Cmd builder (runner contract)
    │   ├── awsflight.go                 ← AWS pre/post-flight checks
    │   ├── timeout.go                   ← per-fetcher timeout escalation
    │   ├── multiinstance.go             ← env-namespaced fan-out
    │   └── real.go                      ← the production runner
    ├── mock/
    │   ├── fetchers.go                  ← Fetcher struct + catalog adapter
    │   ├── runner.go                    ← scripted Beat sequences
    │   ├── runner_adapter.go            ← Runner interface impl
    │   └── fixtures.go                  ← fake stdout used by the scripts
    ├── screens/
    │   ├── welcome.go                   ← profile picker
    │   ├── select.go                    ← fetcher selection
    │   ├── run.go                       ← live run view
    │   └── review.go                    ← results + upload
    └── root/
        ├── model.go                     ← root model: routes between screens
        └── smoke_test.go                ← end-to-end walk
```

### Why `internal/`?

A Go convention: anything under a directory named `internal/` can only be
imported by code under the parent of that `internal/`. So
`internal/runner/...` is importable from anywhere in this module, but if
someone publishes our module and tries to `import
"github.com/.../paramify-fetcher/internal/runner"`, the compiler refuses.

This gives us **structural privacy**: we can refactor `internal/...`
freely because no external code is allowed to depend on it. Even something
as drastic as renaming `runner.FetcherID` to `runner.FID` won't break
anyone outside this repo.

The only thing *not* under `internal/` is `main.go` — Go programs need at
least one `package main` and that has to live where it can be `go run`'d.

### One package per directory

Go's rule: every `.go` file in a directory must declare the same `package`.
Our `internal/runner/` directory has 9 files, all starting with `package
runner`. The package's public API is the union of all
exported (capitalized) names across those files. Lowercase names are
package-private — visible to code in the same directory, invisible to
anyone else.

---

## Part 4 — Go fundamentals you'll see

A whirlwind tour of Go idioms used throughout the codebase, grounded in
files you can open right now.

### Interfaces are duck-typed

```go
// internal/runner/runner.go
type Runner interface {
    Init() tea.Cmd
    Update(msg tea.Msg) (Runner, tea.Cmd)
    Start(ids []FetcherID) tea.Cmd
    Cancel(id FetcherID) tea.Cmd
    Retry(id FetcherID) tea.Cmd
    Bind(s Sender)
}
```

Any type with these six methods *is a* `Runner`. You don't declare "this
type implements Runner" — you just write the methods, and the compiler
checks that the shapes match wherever you try to use the type as a
`Runner`. This is sometimes called **structural typing** or **implicit
satisfaction**.

Both `*mock.MockRunner` (in `internal/mock/runner_adapter.go`) and
`*runner.RealRunner` (in `internal/runner/real.go`) satisfy this
interface. The screens don't know or care which one they have. That's the
"seam" — swap one for the other and nothing else changes.

A common gotcha: if you change the interface, every implementation breaks.
That's a feature, not a bug. The compiler stops you before you ship a
half-migrated change.

### Method receivers: value vs pointer

```go
// internal/runner/types.go
type FetcherID string
func (id FetcherID) String() string { return string(id) }
```

Here `(id FetcherID)` is a *value receiver* — `String()` gets a copy of
the FetcherID. Cheap because FetcherID is a string. Modifying `id`
inside the method wouldn't affect the caller.

```go
// internal/runner/real.go
func (r *RealRunner) Bind(s Sender) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.sender = s
}
```

Here `(r *RealRunner)` is a *pointer receiver* — the method gets a
pointer to the runner, so it can mutate the runner's fields.

**Rule of thumb:** if your method needs to change the receiver, use a
pointer receiver. If your type is large or contains a `sync.Mutex`, use
pointer receivers consistently (a `sync.Mutex` must not be copied; a
value receiver copies it).

Screens in this codebase use *value receivers* on purpose:

```go
// internal/screens/run.go
func (m RunModel) Update(msg tea.Msg) (RunModel, tea.Cmd) { ... }
```

The pattern is "take a model, return a new model." Each `Update` call
*conceptually* produces a new value, even though Go is free to optimize
that to in-place mutation. Bubble Tea does this throughout — it makes
state transitions explicit in the function signature.

### Type aliases keep refactors painless

```go
// internal/mock/fetchers.go
type FetcherID = runner.FetcherID
```

The `=` makes this an *alias* (Go 1.9+). `mock.FetcherID` and
`runner.FetcherID` are *the same type*, not two compatible types. Code
that imports `mock.FetcherID` keeps compiling after we move the
canonical definition to `runner`.

Without the `=`, `type FetcherID runner.FetcherID` would create a *new*
type with the same underlying representation but no implicit
convertibility. Knowing the difference will save you a confused
afternoon.

### Struct tags wire JSON to fields

```go
// internal/catalog/schema.go
type Script struct {
    ID                   string           `json:"id"`
    Name                 string           `json:"name"`
    ScriptFile           string           `json:"script_file"`
    Dependencies         []string         `json:"dependencies"`
    Tags                 []string         `json:"tags"`
    ValidationRules      []ValidationRule `json:"validation_rules"`
    SolutionCapabilities []string         `json:"solution_capabilities"`
    Controls             []string         `json:"controls"`

    Source string `json:"-"` // category key; set by the loader
    Key    string `json:"-"` // script-map key; set by the loader
}
```

The backtick-quoted strings are **struct tags**. The `encoding/json`
package reads them at runtime to map JSON keys to Go fields. `json:"-"`
tells the decoder to skip a field — `Source` and `Key` aren't in the JSON
file; the loader fills them in code. Without `json:"-"`, encoding back to
JSON would emit them as `"Source":"..."` which would be confusing.

Struct tags are just strings — `encoding/json` is one of many packages
that read them. You can add `db:"..."` for a SQL library and `validate:"..."`
for a validator and they'll all coexist on the same field.

### Custom JSON decoding for polymorphic shapes

The catalog has a tricky field: `validation_rules` can be either an
object `{"id":1,"regex":"...","logic":"..."}` or a bare string
`"\"Encrypted\":\\s*true"`. Go's default decoder doesn't know what to do
with that. So we implement our own:

```go
// internal/catalog/schema.go
func (v *ValidationRule) UnmarshalJSON(data []byte) error {
    if len(data) == 0 {
        return nil
    }
    switch data[0] {
    case '"':                       // it's a JSON string
        var s string
        if err := jsonUnmarshal(data, &s); err != nil {
            return err
        }
        v.Regex = s
        return nil
    case '{':                       // it's a JSON object
        var obj validationRuleObject
        if err := jsonUnmarshal(data, &obj); err != nil {
            return err
        }
        v.ID = obj.ID
        v.Regex = obj.Regex
        v.Logic = obj.Logic
        return nil
    default:
        return fmt.Errorf("validation_rules entry must be string or object, got %q", string(data))
    }
}
```

Implementing the `json.Unmarshaler` interface (a method named
`UnmarshalJSON([]byte) error`) takes over decoding for that type. The
caller still writes plain `json.Unmarshal(...)` — the standard library
checks for the method and uses it.

Notice: `data[0]` peeks at the first byte to disambiguate. JSON strings
start with `"`, objects with `{`. Whitespace before that would break us;
the standard library strips it before passing.

### `defer`, the cleanup pattern

```go
// internal/runner/real.go
func (r *RealRunner) execute(id FetcherID, runIdx int) {
    ...
    stdoutF, err := os.Create(filepath.Join(runDir, "stdout.log"))
    if err != nil { /* handle */ }
    defer stdoutF.Close()                           // ← runs when execute returns
    stderrF, err := os.Create(filepath.Join(runDir, "stderr.log"))
    if err != nil { /* handle */ }
    defer stderrF.Close()                           // ← runs when execute returns
    ...
    timeout := ResolveTimeout(script.Key)
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()                                  // ← runs when execute returns
    ...
}
```

`defer X` schedules `X` to run when the surrounding function returns —
no matter how it returns (normal return, panic, anything). It's the Go
equivalent of Python's `with` or Java's try-with-resources, but more
flexible because you can defer any function call.

Deferred calls run in **LIFO order**. So the order above means: first
`cancel()`, then `stderrF.Close()`, then `stdoutF.Close()`. Usually
matches what you want — release in reverse order of acquire.

Common bug to know about: deferred function calls capture variables
**when the defer is evaluated**, not when the deferred call runs:

```go
for _, f := range files {
    defer f.Close()        // ← all closes run AFTER the loop, in LIFO order
}
```

If you have many files, that holds them all open until the function
returns. Solutions: wrap the inner work in its own function so each
defer runs eagerly, or close manually inside the loop.

### Errors with wrapping

Go errors are values, not exceptions. The convention:

```go
// internal/catalog/loader.go
func LoadFile(path string) (*Catalog, []Script, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, nil, fmt.Errorf("open catalog %q: %w", path, err)
    }
    defer f.Close()
    return Load(f)
}
```

The `%w` verb in `fmt.Errorf` *wraps* the original error so callers can
unwrap it later with `errors.Is(...)` or `errors.As(...)`:

```go
// internal/runner/real.go
if errors.Is(ctx.Err(), context.DeadlineExceeded) {
    return StatusFailed, fmt.Sprintf("timed out after %s", timeout)
}
```

`errors.Is` walks the chain of wrapped errors looking for a match. So
even if your error has been wrapped three layers deep, you can still
ask "did this start as a deadline-exceeded?" and get a yes/no answer.

This pattern beats Java-style stack traces because it's explicit:
errors only carry context you put there.

### Context — cancellation and timeouts

The `context.Context` type is Go's standard way to thread cancellation
and deadlines through call stacks:

```go
// internal/runner/real.go
timeout := ResolveTimeout(script.Key)
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()

// ...

cmd := BuildInstanceCmd(ctx, r.cfg, script, inst)    // ← cmd respects ctx
```

`exec.CommandContext` (used inside `BuildInstanceCmd`) hooks the
subprocess to the context: when `ctx` is cancelled or hits its deadline,
the process gets a signal. `cancel()` is the *cancel function* — calling
it explicitly is how you stop early. The `defer cancel()` ensures the
context is released even on the happy path (it'd otherwise leak a
goroutine watching the deadline).

A gotcha: `context.WithTimeout` returns a *cancel function*, not just a
context. You **must** call it (or defer it). Forgetting is a goroutine
leak the linter will catch.

### Mutexes for shared state

The real runner has multiple goroutines (one per running fetcher) that
all read and write to the same maps. So we serialize access with a
`sync.Mutex`:

```go
// internal/runner/real.go
type RealRunner struct {
    cfg    Config
    sender Sender

    mu        sync.Mutex                          // protects the fields below
    states    map[FetcherID]*realState
    queue     []FetcherID
    running   int
    awsAuthOK map[string]bool
    pending   []FinishedMsg

    awsAuthMu sync.Mutex                          // separate, see preflightOK
}
```

The convention: one mutex per "what fields it protects." The comment
above `mu` documents which fields; reading or writing them without
holding `mu` is a bug. Take it with `r.mu.Lock()` and release with
`r.mu.Unlock()` — or `defer r.mu.Unlock()` for symmetry.

A subtle pattern in this file: the runner takes the lock, captures
what it needs into local variables, then releases the lock *before*
calling potentially-slow methods (like `r.send(...)` which writes to
the Bubble Tea event channel):

```go
func (r *RealRunner) sendIfFresh(id FetcherID, runIdx int, msg tea.Msg) {
    r.mu.Lock()
    stale := r.states[id] == nil || r.states[id].runIdx != runIdx
    sender := r.sender
    r.mu.Unlock()                            // ← released before the slow op
    if stale || sender == nil {
        return
    }
    sender.Send(msg)                         // ← slow / blocking
}
```

This avoids holding the lock across operations that might block other
goroutines. It's worth internalizing — the `Lock(); read into locals;
Unlock(); use locals` pattern is everywhere in well-written concurrent
Go.

### `//go:embed` ships files inside the binary

```go
// internal/catalog/embedded.go
import (
    "bytes"
    _ "embed"        // ← side-effect import: enables //go:embed
)

//go:embed embedded/evidence_fetchers_catalog.json
var embeddedJSON []byte

func LoadEmbedded() (*Catalog, []Script, error) {
    return Load(bytes.NewReader(embeddedJSON))
}
```

The `//go:embed` directive is a magic comment (it must be on the line
*directly above* the variable). At compile time, the contents of
`embedded/evidence_fetchers_catalog.json` are read and stuffed into
`embeddedJSON` as a byte slice. The compiled binary has no separate
`.json` file to ship — it's all baked in.

The `_ "embed"` is a *blank import* — we don't reference any names from
the `embed` package, but we need to import it to enable the `//go:embed`
directive. The underscore tells Go "yes I really mean to import this
without using it."

### `iota` and constants

```go
// internal/runner/types.go
type Status int

const (
    StatusQueued Status = iota
    StatusRunning
    StatusOK
    StatusPartial
    StatusFailed
    StatusCancelled
)
```

`iota` is a per-`const`-block counter that starts at 0 and increments by
one each line. So `StatusQueued = 0`, `StatusRunning = 1`, etc. The
`Status` type tag on the first line propagates down the block — every
constant has type `Status`.

This is Go's enum pattern. It's not as rich as a "real" enum (no
auto-generated string method, no exhaustiveness check from the
compiler) — you typically pair it with a `String()` method:

```go
func (s Status) String() string {
    switch s {
    case StatusQueued:    return "queued"
    case StatusRunning:   return "running"
    // ...
    }
    return "?"
}
```

Calling `fmt.Println(s)` will use `String()` automatically because
Go's `fmt` package checks for the `Stringer` interface (any type with
`String() string`).

### Slices, maps, and zero values

Two collection types you'll see everywhere:

```go
queue   []FetcherID                       // slice — ordered, indexable
states  map[FetcherID]*realState          // map — keyed lookup
```

Important: a `nil` slice is a valid empty slice. You can `append` to it,
`range` over it, take its `len`. So the runner's `r.queue = nil` reset
in `Start` works without a separate "is it nil" check.

A `nil` map is read-only. Reading from `nil` returns the zero value;
writing to `nil` panics. So we always initialize maps:

```go
r.states = map[FetcherID]*realState{}      // ← empty but non-nil
```

The Go zero value for a `map` is `nil`, the Go zero value for a `slice`
is also `nil`. The asymmetry (one writable, one not) is one of Go's
quirks — internalize it once and move on.

---

## Part 5 — The Runner interface

This is the most important file in the project. Everything else is
either above it (UI) or below it (specific runner implementations).

```go
// internal/runner/runner.go
type Runner interface {
    Init() tea.Cmd
    Update(msg tea.Msg) (Runner, tea.Cmd)
    Start(ids []FetcherID) tea.Cmd
    Cancel(id FetcherID) tea.Cmd
    Retry(id FetcherID) tea.Cmd
    Bind(s Sender)
}
```

What's special about this:

1. **It mirrors the Bubble Tea Model interface**, on purpose. The runner
   is a state machine that reacts to messages and produces commands —
   same shape as a screen, just at a different layer. The Run screen
   forwards every `tea.Msg` it sees to `runner.Update()` so the runner
   can advance its own scheduling without a separate event loop.

2. **`Start`, `Cancel`, `Retry` return `tea.Cmd`**, not just plain
   methods. Following the rule from Part 2: don't *do* things, *describe*
   them. The screen says "I want to cancel"; the runner says "here's a
   Cmd that performs the cancel"; Bubble Tea executes the Cmd later.

3. **`Bind(Sender)` is the goroutine bridge.** The mock runner doesn't
   need it (it's pure tea.Cmd). The real runner has subprocess
   pipe-reader goroutines that need to push `OutputMsg` back into the
   event loop from outside Update. Sender is the seam for that.

### Sender — the smallest possible interface

```go
// internal/runner/sender.go
type Sender interface {
    Send(msg tea.Msg)
}
```

That's the entire interface — one method. Why so small?

Because the only thing the runner needs is a way to push a message back
into Bubble Tea. `*tea.Program` has dozens of other methods we don't
care about; depending on the whole `*tea.Program` would couple us to
their entire surface area. With a one-method interface, tests can pass
in a fake:

```go
type fakeSender struct {
    received []tea.Msg
}
func (s *fakeSender) Send(msg tea.Msg) {
    s.received = append(s.received, msg)
}
```

This is the **interface segregation principle** distilled: depend on
what you *use*, not what's *available*.

### The public/private message split

```go
// internal/runner/messages.go      ← public, anyone can produce/consume
type StartedMsg  struct{ ID FetcherID }
type OutputMsg   struct{ ID FetcherID; Line string }
type FinishedMsg struct{ ID FetcherID; Status Status; ExitCode int; ErrorReason string }
type TargetsMsg  struct{ Targets []Target }
type StallTickMsg struct{}
```

```go
// internal/mock/runner_adapter.go  ← private, never leaves this package
type beatMsg       struct{ ID runner.FetcherID; Index int; Run int }
type finishTickMsg struct{ ID runner.FetcherID; Run int }
type cancelMsg     struct{ ID runner.FetcherID }
type retryMsg      struct{ ID runner.FetcherID }
```

Lowercase = unexported = invisible to other packages. The mock runner
uses `beatMsg` and `finishTickMsg` to drive its scripted playback; nobody
else ever sees these. The real runner has its own private internals;
they don't even share the same names. But both runners produce the *same*
public messages (`StartedMsg`, `OutputMsg`, `FinishedMsg`) — that's the
contract the screen depends on.

This split is what lets us have two completely different
implementations behind the same interface.

---

## Part 6 — The Mock runner

`internal/mock/runner_adapter.go` is the deterministic, scripted runner.
It exists to make the demo work and to let us write tests without spawning
real processes.

### Beats: the scripted output

```go
// internal/mock/runner.go
type Beat struct {
    Delay time.Duration
    Line  string
}

type Script struct {
    Beats      []Beat
    FinalDelay time.Duration
    Final      runner.Status
    ExitCode   int
}
```

A "Beat" is "wait Delay, then emit Line." A Script is a sequence of Beats
followed by a terminal state. `Build(fetcher)` returns a Script
deterministically based on the fetcher's `BehaviorKind` — so
"BehaviorPartial" always produces a script that emits two `WARN:` lines
and exits with `StatusPartial`.

### Why no goroutines?

The mock runner has zero `go func() {...}` statements anywhere. Every
"wait then emit" is implemented as a `tea.Tick`:

```go
// internal/mock/runner_adapter.go
func scheduleBeat(id runner.FetcherID, idx, run int, delay time.Duration) tea.Cmd {
    return tea.Tick(delay, func(time.Time) tea.Msg {
        return beatMsg{ID: id, Index: idx, Run: run}
    })
}
```

`tea.Tick(delay, f)` returns a Cmd that, when run, sleeps for `delay`
and then calls `f` to produce a Msg. The Msg comes back through the
normal Update loop. So "wait 350ms then output a line" is just:

1. Update returns `scheduleBeat(...)` as a Cmd.
2. Bubble Tea runs the Cmd, which calls `tea.Tick`, which waits and
   returns a `beatMsg`.
3. `beatMsg` arrives as a Msg in the next Update call.
4. Update emits the line as an `OutputMsg` and schedules the next beat.

```go
// internal/mock/runner_adapter.go
case beatMsg:
    st, ok := m.states[msg.ID]
    if !ok || msg.Run != st.runIdx || st.status != runner.StatusRunning {
        return m, nil                              // stale or cancelled
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
```

This is clean for two reasons:

- **Determinism.** A test can replace `tea.Tick` with an immediate
  function and the script plays out as fast as the test runner can
  process Updates. No `time.Sleep`, no flakiness.

- **No goroutine leaks.** No background routines to clean up on cancel.
  Cancel just sets a flag; subsequent stale `beatMsg`s see it and
  silently drop themselves.

### `runIdx` — invalidating stale ticks

When the user retries a cancelled fetcher, there might still be a
`beatMsg` in flight from the previous attempt. We don't want it to
accidentally print an old line. The fix:

```go
type mockState struct {
    id      runner.FetcherID
    fetcher Fetcher
    script  Script
    beatIdx int
    runIdx  int                    // ← incremented on cancel/retry
    status  runner.Status
}

case beatMsg:
    st, ok := m.states[msg.ID]
    if !ok || msg.Run != st.runIdx || st.status != runner.StatusRunning {
        return m, nil              // stale: drop it
    }
```

Every scheduled tick carries the `runIdx` it was scheduled under. When
a tick arrives, we compare against the current state. Mismatch → drop
silently. No need to chase down and cancel pending Cmds. This pattern
recurs in the real runner too.

---

## Part 7 — The Real runner

`internal/runner/real.go` is the production runner: it spawns
subprocesses, captures stdout/stderr, runs AWS pre-flight checks,
classifies results, writes log files. It uses goroutines because
subprocesses are blocking.

### The lifecycle of one fetcher run

```
        Start(ids)                                        ┌── pipeReader (stdout)
            │                                             │
            ▼                                             │
   queue ─►─── fillRunning ──► spawn execute() goroutine ─┤── pipeReader (stderr)
                                       │                  │
                                       ▼                  │
                              cmd.Start() and             │
                              wait for both pipes ────────┘
                                       │
                                       ▼
                                  cmd.Wait()
                                       │
                                       ▼
                            classify(exit, stderr, post-flight)
                                       │
                                       ▼
                              FinishedMsg → screen
                                       │
                                       ▼
                                   advance() — refill the queue
```

### Spawning the subprocess

```go
// internal/runner/exec.go
func BuildInstanceCmd(ctx context.Context, cfg Config, s catalog.Script, inst Instance) *exec.Cmd {
    scriptPath := filepath.Join(cfg.FetcherRepoRoot, s.ScriptFile)

    prog := "bash"
    if filepath.Ext(scriptPath) == ".py" {
        prog = "python3"
    }

    args := []string{
        scriptPath,
        profile, region, outDir, "/dev/null",       // 4 positional args (legacy contract)
    }
    if profile != "" {
        args = append(args, "--profile", profile)   // modern flag form
    }
    // ...
    cmd := exec.CommandContext(ctx, prog, args...)
    cmd.Dir = cfg.FetcherRepoRoot
    cmd.Env = env
    return cmd
}
```

`exec.CommandContext` ties the subprocess's lifetime to the context:
when `ctx` is cancelled or times out, Go signals the process. We use
this for both per-fetcher timeouts (`context.WithTimeout`) and explicit
cancel (`context.WithCancel` + storing the cancel function).

The 4 positional arguments + `/dev/null` are *legacy contract* — the
existing fetcher scripts expect `<profile> <region> <output_dir>
<legacy_csv_path>` followed by `--flag`-style arguments. We can't
change the script side without breaking the world; the runner adapts.

### Pipe readers: the goroutine bridge

```go
// internal/runner/real.go
func (r *RealRunner) pipeReader(wg *sync.WaitGroup, id FetcherID, runIdx int, src io.Reader, sink io.Writer) {
    defer wg.Done()
    scanner := bufio.NewScanner(src)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)        // grow to 1 MB
    for scanner.Scan() {
        line := scanner.Text()
        fmt.Fprintln(sink, line)                               // tee to log file
        r.sendIfFresh(id, runIdx, OutputMsg{ID: id, Line: line})
    }
}
```

This runs as its own goroutine. `bufio.Scanner` does the line-splitting
for us; the default buffer is 64KB (some JSON dumps from fetchers are
larger, so we bump to 1MB).

Each line is written to a log file (`tee`) AND pushed back into Bubble
Tea via `sendIfFresh`. The screen sees it as an `OutputMsg`.

```go
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
```

Same `runIdx` invalidation as the mock — if the user cancelled and
retried while output was still streaming, the in-flight goroutine's
late lines silently drop.

### Goroutine + WaitGroup synchronization

```go
// internal/runner/real.go (inside execute)
var wg sync.WaitGroup
wg.Add(2)
go r.pipeReader(&wg, id, runIdx, stdout, stdoutF)
go r.pipeReader(&wg, id, runIdx, stderr, stderrF)
wg.Wait()                                          // ← block here until both pipes EOF

waitErr := cmd.Wait()                              // ← then reap the process
```

`sync.WaitGroup` is a counter you can wait on. `Add(2)` means "I'm
launching 2 goroutines"; each goroutine calls `wg.Done()` (we do it
via `defer wg.Done()` at the top of `pipeReader`). `wg.Wait()` blocks
until the counter reaches zero.

The order matters: we drain stdout/stderr *before* calling
`cmd.Wait()`. Reversed, the subprocess might block writing to a full
pipe buffer and never exit. This is one of those Unix subtleties that
surprises everyone who hasn't been bitten by it.

### Why memoize AWS pre-flight?

```go
func (r *RealRunner) preflightOK(profile, region string) bool {
    key := profile + "\x00" + region
    r.mu.Lock()
    if ok, found := r.awsAuthOK[key]; found {
        r.mu.Unlock()
        return ok
    }
    r.mu.Unlock()

    r.awsAuthMu.Lock()                            // serialize the actual check
    defer r.awsAuthMu.Unlock()

    // double-check: another goroutine may have populated while we waited
    r.mu.Lock()
    if ok, found := r.awsAuthOK[key]; found {
        r.mu.Unlock()
        return ok
    }
    r.mu.Unlock()

    // ...actually run `aws sts get-caller-identity`...
}
```

Running `aws sts get-caller-identity` takes ~500ms. If you have 12 AWS
fetchers selected, you don't want to do that 12 times. So we memoize
the result for the duration of one `Start()` call.

The two-mutex pattern (the *double-checked locking* idiom) deserves
attention:

- `awsAuthMu` ensures only ONE goroutine actually shells out, even if
  three concurrent fetchers all arrive at preflightOK at once.
- The inner `mu.Lock()` after acquiring `awsAuthMu` re-checks the
  cache, in case another goroutine populated it while we waited.
- `mu` (the runner-wide mutex) protects the cache map; `awsAuthMu` is
  separate so the rest of the runner doesn't block on a slow `aws`
  call.

Don't write this from scratch unless you know why each lock is there.
Most of the time you want the simple "one mutex, hold it briefly"
pattern. This is the exception.

---

## Part 8 — The Catalog

The catalog (`evidence_fetchers_catalog.json`) is the contract between
the fetcher scripts, our UI, and Paramify. We parse it, validate it,
and ship it inside the binary.

### Embedding

```go
// internal/catalog/embedded.go
//go:embed embedded/evidence_fetchers_catalog.json
var embeddedJSON []byte
```

Two things:

1. The `//go:embed` line *must* be directly above `var embeddedJSON`.
   Other comments break it.
2. The path is relative to the source file, not the working directory.

Result: every binary we ship has the catalog inside it. `--catalog
<path>` overrides for development:

```go
// internal/mock/fetchers.go
if catOverride != "" {
    _, scripts, err = catalog.LoadFile(catOverride)
} else {
    _, scripts, err = catalog.LoadEmbedded()
}
```

### Validation as a first-class step

The loader doesn't just decode JSON — it validates structure:

```go
// internal/catalog/loader.go
var idShape = regexp.MustCompile(`^EVD-[A-Z0-9]+(-[A-Z0-9]+)+$`)

func flatten(c *Catalog) ([]Script, error) {
    seen := map[string]string{}
    // ...
    for _, ck := range categoryKeys {
        cat := c.Wrapper.Categories[ck]
        // ...
        for _, sk := range scriptKeys {
            s := cat.Scripts[sk]
            s.Source = ck
            s.Key = sk
            if !idShape.MatchString(s.ID) {
                return nil, fmt.Errorf("invalid id %q at %s/%s: must match EVD-<UPPER>-<UPPER>", s.ID, ck, sk)
            }
            if where, dup := seen[s.ID]; dup {
                return nil, fmt.Errorf("duplicate id %q: first at %s, again at %s/%s", s.ID, where, ck, sk)
            }
            seen[s.ID] = ck + "/" + sk
            // ...
        }
    }
}
```

Every fetcher ID must match `EVD-<UPPER>-<UPPER>` and must be globally
unique. These are non-obvious requirements that come from Paramify's
API: the ID is the idempotency key for "is this the same Evidence Set
as last time?" Renaming would create duplicates in customer tenants.

The validator catches violations at **startup**, before the user can
interact with the UI. That's better than catching them at upload time
when you've already wasted 5 minutes running fetchers.

### Determinism via sorting

```go
sort.Strings(categoryKeys)
// ...
sort.SliceStable(staged, func(i, j int) bool { return staged[i].ID < staged[j].ID })
```

Maps in Go iterate in **random order** — the runtime deliberately
randomizes to prevent code from relying on a particular order. So if
we want stable output (for reproducible builds, screenshot-stable
demos, deterministic tests), we sort.

`sort.Strings` sorts a `[]string` in place. `sort.SliceStable` takes a
slice and a less-function; *Stable* means equal-keyed elements keep
their relative order.

---

## Part 9 — The screens

Each screen is its own `tea.Model`. They have local state and don't
import each other. They communicate via *messages* — typed structs
that bubble up to the root model.

### Welcome — the simplest

```go
// internal/screens/welcome.go (sketch)
type WelcomeModel struct {
    profiles []Profile
    cursor   int
    keys     app.KeyMap
}

type SelectedProfileMsg struct{ Profile Profile }

func (m WelcomeModel) Update(msg tea.Msg) (WelcomeModel, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch {
        case key.Matches(msg, m.keys.Up):
            if m.cursor > 0 { m.cursor-- }
        case key.Matches(msg, m.keys.Down):
            if m.cursor < len(m.profiles)-1 { m.cursor++ }
        case key.Matches(msg, m.keys.Enter):
            return m, func() tea.Msg {
                return SelectedProfileMsg{Profile: m.profiles[m.cursor]}
            }
        }
    }
    return m, nil
}
```

Things worth noticing:

- **Type switch** (`switch msg := msg.(type)`). We don't know what kind
  of message we got; we case on the type. The new variable `msg` inside
  each case is typed as `tea.KeyMsg` (or whatever).
- **Closure-as-Cmd**: `return m, func() tea.Msg { return SelectedProfileMsg{...} }` —
  the function literal is the command. When run, it produces the
  message.
- **No global state.** The cursor is an `int` field on the model.

### Select — multi-pane with filter

The Select screen has two panes (sources tree, fetcher table), a
filter input, and multi-select. State is more interesting:

```go
type SelectModel struct {
    catalog    []mock.Fetcher
    sources    []string
    focused    pane                   // sources or fetchers
    sourceIdx  int
    fetcherIdx int
    selected   map[mock.FetcherID]bool
    filter     textinput.Model        // a Bubbles component
    filterMode bool
}
```

The `textinput.Model` is from `github.com/charmbracelet/bubbles` — a
collection of pre-built UI components for Bubble Tea. We compose it
into our model. When in filter mode we forward keystrokes to it:

```go
if m.filterMode {
    switch msg.String() {
    case "esc":
        m.filterMode = false
        m.filter.Blur()
        m.filter.SetValue("")
        // ...
    }
    var cmd tea.Cmd
    m.filter, cmd = m.filter.Update(msg)
    return m, cmd
}
```

The composability is the point: `textinput.Model` is also a `tea.Model`,
so its `Update` returns the same shape. We thread it through.

### Run — the one with the runner

The Run screen is the most complex. It owns:

- The list of fetcher IDs to run
- A `runner.Runner` (mock or real)
- Per-fetcher state (status, output buffer, timing)
- A scrolling viewport for the focused fetcher's output
- A spinner, progress bar, stall ticker

The interesting bits:

```go
// internal/screens/run.go
func (m RunModel) Init() tea.Cmd {
    return tea.Batch(
        m.spinner.Tick,
        runner.ScheduleStallTick(),
        m.runner.Init(),
        m.runner.Start(m.targets),
    )
}
```

`Init` returns a *batch* of commands to run at startup. `tea.Batch`
combines multiple Cmds into one — Bubble Tea runs them concurrently
and feeds the resulting messages back into Update one at a time.

```go
case runner.OutputMsg:
    if st, ok := m.states[msg.ID]; ok {
        st.output = append(st.output, msg.Line)
        if len(st.output) > maxOutputLines {
            st.output = st.output[len(st.output)-maxOutputLines:]    // ring-cap
        }
        st.lastOutputAt = time.Now()
        st.stalled = false
    }
```

Output is appended to a per-fetcher slice, then truncated if it grows
past the cap. The slicing trick (`st.output[len(st.output)-N:]`) keeps
the *last* N lines — the most recent.

```go
// Forward all messages to the runner so it can advance its internal scheduling
var rcmd tea.Cmd
m.runner, rcmd = m.runner.Update(msg)
cmds = append(cmds, rcmd)
```

The Run screen forwards every message it sees to the runner. The
runner's Update ignores messages it doesn't care about. This is the
"the runner is also a state machine" pattern from Part 5.

### Review — paged table

```go
// internal/screens/review.go
type ReviewModel struct {
    keys    app.KeyMap
    results []RunResult
    cursor  int
    offset  int                    // for paging
    phase   uploadPhase
    progress progress.Model
}
```

`offset` is the index of the first row visible on screen — we render
a page of 10 rows starting at `offset`. As the user moves the cursor
past the visible window, `offset` slides to keep the cursor in view:

```go
// Keep cursor within the 10-row window.
if m.cursor < m.offset {
    m.offset = m.cursor
} else if m.cursor >= m.offset+pageSize {
    m.offset = m.cursor - (pageSize - 1)
}
```

This is the cheapest possible "virtualized list" — we always render
exactly 10 rows regardless of how many results there are.

---

## Part 10 — Wiring

### The root model

```go
// internal/root/model.go
type Model struct {
    keys    app.KeyMap
    screen  Screen                  // which sub-model is active
    runner  runner.Runner           // shared across sub-models that need it

    welcome screens.WelcomeModel
    sel     screens.SelectModel
    run     screens.RunModel
    review  screens.ReviewModel
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Global escape hatches
    if k, ok := msg.(tea.KeyMsg); ok {
        if k.String() == "ctrl+c" { return m, tea.Quit }
        // ...
    }

    switch msg := msg.(type) {
    case screens.SelectedProfileMsg:
        m.sel = screens.NewSelect(m.keys, m.profile).Resize(m.width, m.height)
        m.screen = ScreenSelect
        return m, m.sel.Init()

    case screens.SelectionConfirmedMsg:
        m.run = screens.NewRun(m.keys, m.profile, msg.IDs, m.runner).Resize(m.width, m.height)
        m.screen = ScreenRun
        return m, m.run.Init()

    case screens.RunCompleteMsg:
        m.review = screens.NewReview(m.keys, m.profile, msg.Results).Resize(m.width, m.height)
        m.screen = ScreenReview
        return m, m.review.Init()
    }

    // Otherwise: route to the active screen
    switch m.screen {
    case ScreenWelcome: m.welcome, cmd = m.welcome.Update(msg)
    case ScreenSelect:  m.sel, cmd     = m.sel.Update(msg)
    case ScreenRun:     m.run, cmd     = m.run.Update(msg)
    case ScreenReview:  m.review, cmd  = m.review.Update(msg)
    }
}
```

The root does two things:

1. Catches **transition messages** (the `Selected*Msg` / `*ConfirmedMsg`
   /  `*CompleteMsg` types each screen produces) and switches the
   active screen.
2. Forwards everything else to the currently active sub-model.

This is the **finite state machine** pattern at the screen level, on
top of the FSM pattern at the runner level. Composable, easy to test —
each screen can be exercised in isolation.

### `main.go` — flag parsing and runner wiring

```go
// main.go
func main() {
    demo := flag.Bool("demo", true, "use the deterministic mock runner")
    catalogPath := flag.String("catalog", "", "override the embedded catalog (development)")
    profile := flag.String("profile", "", "AWS profile (real runner)")
    region := flag.String("region", "", "AWS region (real runner)")
    repoRoot := flag.String("fetcher-repo-root", "", "path to evidence-fetchers checkout (real runner)")
    flag.Parse()

    if *catalogPath != "" {
        mock.SetCatalogOverride(*catalogPath)
    }
    if err := mock.EnsureCatalog(); err != nil {
        die(2, "catalog error: %v", err)
    }

    var r runner.Runner
    if *demo {
        r = mock.NewMockRunner(mock.Catalog())
    } else {
        r = buildRealRunner(*profile, *region, *repoRoot, *catalogPath)
    }

    p := tea.NewProgram(root.New(r), tea.WithAltScreen(), tea.WithMouseCellMotion())
    r.Bind(p)                                       // ← the goroutine bridge
    if _, err := p.Run(); err != nil { ... }
}
```

The order matters:

1. **Parse flags first.** Don't open the alt-screen if the user passed
   `--help`; let `flag.Parse` handle that.
2. **Validate catalog before starting the TUI.** A catalog-error message
   would be invisible if we'd already entered alt-screen mode.
3. **Pick a runner.** Just an interface variable.
4. **Wire it into the program.** `tea.NewProgram` creates the program;
   `r.Bind(p)` gives the runner a way to push messages from goroutines.
5. **Run.** `p.Run` blocks until the program exits.

The `--demo` flag defaults to `true` because the prototype's most
common use is the demo. To run for real you'd pass
`--demo=false --fetcher-repo-root=/path/to/evidence-fetchers
--profile=...`.

---

## Part 11 — Why we did it this way

A few questions a reader might have, with answers.

### Q: Why an interface for the runner? Why not just one type?

Because we have two kinds of execution that look completely different
underneath but produce identical UI behavior:

- The mock is `tea.Tick`-driven, single-threaded, deterministic.
- The real one spawns subprocesses, runs N goroutines in parallel,
  needs context cancellation and pipe buffering.

If we picked one and lived with it, either:

- Tests and the demo would need real AWS creds (insane), or
- The real runner would be limited to the mock's primitives (would
  invent itself anyway, just under a different name).

The interface is the contract — a tiny artifact whose existence makes
two unrelated implementations coexist.

### Q: Why goroutines in the real runner but not the mock?

`os/exec`'s pipe APIs (`cmd.StdoutPipe()`) return blocking readers.
You *have* to read them on a separate goroutine; otherwise the
subprocess will eventually block on a full pipe buffer and never exit.
Goroutines are the price of admission for subprocess management in Go.

The mock doesn't have that constraint — its "output" is just strings
in a slice, and `tea.Tick` provides the only delay primitive it needs.
Adding goroutines to it would buy us nothing and complicate cancel.

### Q: Why no channels in the runner?

You'd think "stream of output → channel" but Bubble Tea already gives
us a stream-of-messages abstraction. Routing output through a separate
channel and then *back* into Bubble Tea's message loop would be two
levels of indirection. `Sender.Send` is the one-level version: the
goroutine pushes directly into the event loop.

Channels show up in some other places (the test helpers, for
instance), but for this code path `Sender` is the right primitive.

### Q: Why pointer receivers on the runners but value receivers on the screens?

The runners hold mutable state that must persist across calls
(in-flight subprocess handles, the queue, the pre-flight cache).
Pointer receivers let methods mutate that state in place.

The screens conceptually create a new model on every Update — a value
receiver makes that explicit. Go is free to optimize value receivers
to in-place mutation, but the *contract* is "given old state and
message, return new state." It also keeps screens from accidentally
sharing state with each other through pointers.

### Q: Why bake the catalog into the binary instead of reading it from disk at runtime?

So we ship one file. Customers don't have to clone our repo, set a
`PARAMIFY_CATALOG_PATH` env var, or worry about the binary getting out
of sync with the catalog. `--catalog <path>` is the override for
developers; production users get the embedded copy.

This pattern (`//go:embed` + an override flag) is common for shipping
HTML templates, config defaults, SQL migrations, etc.

### Q: Why not use `github.com/spf13/cobra` for flag parsing?

The prototype only has 5 flags, all at the top level. `flag` from the
standard library handles that in 6 lines. Adding a third-party CLI
framework would dwarf the actual feature it supports. We can adopt
cobra later if subcommands appear (`paramify-fetcher run`,
`paramify-fetcher list-presets`, etc.).

This is a recurring theme: **prefer the standard library** when it's
sufficient. Every dependency is a vector for breakage on someone
else's release schedule.

### Q: What's the testing story?

The smoke test in `internal/root/smoke_test.go` walks the entire
program through every screen by feeding messages directly into the
root model's `Update`:

```go
func TestSmokeWalk(t *testing.T) {
    r := mock.NewMockRunner(mock.Catalog())
    var m tea.Model = New(r)

    m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
    if v := m.View(); !strings.Contains(v, "select a profile") { t.Fatal(...) }

    m, _ = m.Update(screens.SelectedProfileMsg{Profile: ...})
    if v := m.View(); !strings.Contains(v, "fetchers") { t.Fatal(...) }

    m, _ = m.Update(screens.SelectionConfirmedMsg{IDs: ...})
    // ...
}
```

This is possible *because* Bubble Tea's `Update` is pure and the
runner is an interface. No terminal needed, no subprocesses, no time
passes. It's a perfectly legitimate "integration test" that runs in
under a second.

For a real-world TUI you'd add: golden-file snapshot tests for `View()`
output, fixture-based replay for the runner (record real fetcher
output once, replay through the mock), and contract tests that
validate every fetcher script's output JSON against the catalog's
expectations.

### Q: How is this "beginner to intermediate"?

Beginner-level Go you'll encounter in this codebase:

- Packages, imports, struct types
- Methods, value vs pointer receivers
- Interfaces (declared by what you need, satisfied by what you have)
- Error handling (`if err != nil { return err }`)
- `defer` for cleanup
- `range` over slices and maps
- Switch / type-switch
- Goroutines and `sync.Mutex` (just enough)

Intermediate-level Go that shows up in a few places:

- `context.Context` for cancellation
- `bufio.Scanner` for line-oriented I/O
- `//go:embed` for resource bundling
- Custom `UnmarshalJSON` for polymorphic JSON
- `sync.WaitGroup` for "wait for N goroutines"
- The double-checked locking pattern (rarely; only in `preflightOK`)

Things you won't see (yet) and don't need to learn for this project:

- Generics (Go 1.18+) — we don't use them; the codebase predates the
  habit.
- Channels-as-iterators / pipeline patterns — overkill here.
- `reflect` package — never needed for application code.

---

## Part 12 — Reading order

If your goal is...

- **"Understand the architecture in 30 minutes"** — read this doc, then
  open `internal/runner/runner.go` and `internal/root/model.go`. That's
  it.

- **"Add a new screen (e.g., a Help screen)"** — read
  `internal/screens/welcome.go` (simplest) and
  `internal/root/model.go`. Pattern: new struct under
  `internal/screens/`, new screen constant + case in the root, new
  transition message.

- **"Add a new key binding"** — `internal/app/keys.go` (declare it),
  then handle it in whichever screen wants it. Update the footer hint
  list in that screen's `View`.

- **"Understand how runs work end-to-end"** — read in this order:
  1. `internal/runner/runner.go` (the interface)
  2. `internal/runner/messages.go` (the public message types)
  3. `internal/screens/run.go` (the consumer)
  4. `internal/mock/runner_adapter.go` (one implementation)
  5. `internal/runner/real.go` (the other implementation)

- **"Change how the catalog is loaded"** —
  `internal/catalog/{schema,loader,embedded}.go`. The decoder is in
  `schema.go`; the validation is in `loader.go`; the embed directive
  is in `embedded.go`.

- **"Add a new behavior to the mock (a new failure mode)"** — add a
  constant to the `BehaviorKind` enum in `internal/mock/fetchers.go`,
  add a case to `Build` in `internal/mock/runner.go`, optionally add
  an entry to `behaviorOverrides` to assign it to a specific fetcher.

- **"Wire the real runner into a deployment"** — read `main.go`'s
  `buildRealRunner`, then `internal/runner/exec.go`, then
  `internal/runner/real.go`. The Phase 4–7 sections of `DESIGN.md`
  cover what's still missing.

- **"Write a test"** — start with `internal/root/smoke_test.go` for the
  shape, then look at how it constructs a runner and feeds messages.
  Most tests in this codebase don't need anything more elaborate.

---

## Appendix: a glossary of the project's own jargon

| Term | What it means here |
|---|---|
| **Fetcher** | A bash or python script in `evidence-fetchers/fetchers/<source>/` that gathers compliance evidence. |
| **Catalog** | The JSON file (`evidence_fetchers_catalog.json`) listing every fetcher with its metadata. The contract between scripts, our UI, and Paramify. |
| **EVD-* ID** | The stable `EVD-<CATEGORY>-<NAME>` identifier for a fetcher. The idempotency key for Paramify's API. |
| **Source** | A category of fetchers — `aws`, `gitlab`, `okta`, `k8s`, etc. |
| **Behavior** | (Mock only) Which scripted scenario a fetcher demos: Quick / Normal / Streaming / SlowStart / Stall / Partial / HardFail. |
| **Beat** | (Mock only) A single `(delay, line)` pair in a script. |
| **Run** | One execution of N selected fetchers, end to end. Produces an evidence directory and a summary. |
| **Instance** | (Multi-instance only) One concrete execution of a base fetcher against a specific tenant/region/project. `EVD-FOO-BAR_region_2`, etc. |
| **Pre-flight** | The AWS auth check (`aws sts get-caller-identity`) we run before any AWS fetcher. Catches the "I forgot to log in" footgun. |
| **Post-flight** | After-the-fact validation that an exit-0 run actually produced valid evidence (no `metadata.account_id="unknown"`). |
| **Stall** | UI state for a running fetcher that hasn't produced output for `StallThreshold` (4s). Display-only — the fetcher is still "running." |
| **Status** | Terminal lifecycle state of a fetcher: queued / running / ok / partial / failed / cancelled. |
| **runIdx** | Per-fetcher counter incremented on cancel/retry. Used to invalidate stale messages from prior attempts. |
| **Sender** | Smallest interface that lets a goroutine push messages into Bubble Tea's event loop. Implemented by `*tea.Program`. |
| **Preset** | (Future) A saved, named selection of fetchers — "FedRAMP Low", "Customer ACME". Stored as TOML. |
