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
9. [The screens — Welcome, Secrets, Select, Run, Review](#part-9--the-screens)
10. [Secrets, output, preflight, uploader — the support packages](#part-10--support-packages)
11. [Wiring — root model and `main.go`](#part-11--wiring)
12. [Why we did it this way](#part-12--why-we-did-it-this-way)
13. [Reading order — where to start for common tasks](#part-13--reading-order)

---

## Part 1 — The 30,000-foot view

`paramify-fetcher` is a terminal UI ("TUI") that walks an operator through
collecting compliance evidence from cloud platforms, then uploading it to
Paramify. The actual data collection is done by ~70 bash and Python scripts
that already exist in a sibling repo (`evidence-fetchers/`). Our job is the
*UI* and the *orchestration*: pick which scripts to run, run them in
parallel, stream their output, classify their results, package the artifacts.

The program is built around five screens:

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Welcome  ───►  Select  ───►  Run  ───►  Review  ──►  Paramify upload    │
│  pick a profile pick fetchers stream    summary +     (real, via Python  │
│  + AWS pre-      + filter      output   evidence dir   pusher subprocess)│
│  flight check                                                            │
│       │                                                                  │
│       └──── Secrets (s) ◄──── any screen can detour to set keys ─────┐  │
│                                                                       │  │
│  Review re-routes to Secrets if PARAMIFY_UPLOAD_API_TOKEN is missing ─┘  │
└──────────────────────────────────────────────────────────────────────────┘
```

There are two "runners" — concrete implementations of one interface — that
turn fetcher selections into running work:

- **Mock runner**: produces deterministic, scripted output ("ok: 12
  records"). No subprocess, no AWS, no network. Powers the demo, the tests,
  and screenshots.
- **Real runner**: invokes actual scripts via `os/exec`, captures
  stdout/stderr line-by-line, runs an AWS pre-flight check (cached on
  disk), expands selections into multi-instance targets, classifies
  results, writes per-fetcher log files and a `summary.json`. The thing
  that ships.

A separate **uploader** package wraps the existing
`evidence-fetchers/2-create-evidence-sets/paramify_pusher.py` script so the
Review screen can do a real upload — not a mock animation — when an
upload token is configured.

The whole thing is one statically-linked Go binary with the catalog of
fetchers (`evidence_fetchers_catalog.json`) baked in via `//go:embed`.
Operators don't need a separate `paramify_pusher` install — but Python 3
is required at runtime for live mode (the fetcher scripts and the upload
pusher are Python; we shell out).

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
├── main.go                              ← program entrypoint, flag parsing,
│                                          runner+uploader+secrets wiring
├── go.mod / go.sum                      ← Go module metadata
├── DESIGN.md                            ← product design doc
├── README.md                            ← short user-facing intro
├── WALKTHROUGH.md                       ← this file
├── docs/                                ← architecture truths + diagrams
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
    │   ├── runner.go                    ← Runner + ProfileConfigurer interfaces
    │   ├── messages.go                  ← public msgs (Started/Output/Targets/
    │   │                                   Finished/StallTick) + Target struct
    │   ├── types.go                     ← FetcherID, Status, StallThreshold
    │   ├── sender.go                    ← Sender interface (goroutine bridge)
    │   ├── exec.go                      ← Config + exec.Cmd builder
    │   ├── awsflight.go                 ← AuthChecker iface + AWS pre/post-flight
    │   ├── timeout.go                   ← FETCHER_TIMEOUT + ssllabs floor
    │   ├── multiinstance.go             ← GITLAB_PROJECT_N / AWS_REGION_N fan-out
    │   ├── summary.go                   ← summary.json writer (Python schema)
    │   └── real.go                      ← the production runner
    ├── mock/
    │   ├── fetchers.go                  ← Fetcher struct + catalog adapter
    │   ├── runner.go                    ← scripted Beat sequences
    │   ├── runner_adapter.go            ← Runner interface impl
    │   └── fixtures.go                  ← fake stdout used by the scripts
    ├── secrets/
    │   ├── store.go                     ← Store interface + key constants
    │   ├── env.go                       ← read-only Store backed by os.Environ
    │   ├── keychain.go                  ← OS keychain Store (macOS Keychain etc.)
    │   ├── memory.go                    ← in-memory Store (tests)
    │   ├── merged.go                    ← Primary+Fallback (keychain → env)
    │   ├── environ.go                   ← BuildEnviron: merge store into child env
    │   └── requirements.go              ← per-source secret metadata table
    ├── output/
    │   ├── paths.go                     ← XDG/PARAMIFY_FETCHER_HOME resolution
    │   └── session.go                   ← SessionLog + SenderTap (msg → log)
    ├── preflight/
    │   ├── tools.go                     ← `which` checks for aws/jq/bash/…
    │   ├── profiles.go                  ← parse ~/.aws/config for profiles
    │   └── service.go                   ← cached AWS auth + SSO login runner
    ├── uploader/
    │   ├── uploader.go                  ← Uploader interface
    │   ├── python.go                    ← shells out to paramify_pusher.py
    │   └── paramify.go                  ← Go HTTP client (alternative path)
    ├── evidence/
    │   ├── evidence_sets.go             ← writes evidence_sets.json per run
    │   └── instructions_api.go          ← optional Paramify instructions fetch
    ├── screens/
    │   ├── welcome.go                   ← profile picker + AWS preflight
    │   ├── secrets.go                   ← key editor (catalog-source + focused mode)
    │   ├── select.go                    ← fetcher selection
    │   ├── run.go                       ← live run view
    │   └── review.go                    ← results + real Paramify upload
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

// Optional secondary interface — only the real runner implements it.
type ProfileConfigurer interface {
    ConfigureProfile(profile, region string)
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

`ProfileConfigurer` is an example of **interface upgrading**: the root
model takes a generic `Runner`, but when the user picks a profile on
Welcome it does a type assertion:

```go
// internal/root/model.go
if configurable, ok := m.runner.(runner.ProfileConfigurer); ok {
    configurable.ConfigureProfile(msg.Profile.Name, msg.Profile.Region)
}
```

The mock doesn't need profiles, so it doesn't implement
`ProfileConfigurer` — and the assertion harmlessly fails. The real runner
does, so its `cfg.Profile` / `cfg.Region` get updated before `Start()`.

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
type StartedMsg   struct{ ID FetcherID }
type OutputMsg    struct{ ID FetcherID; Line string }
type FinishedMsg  struct{ ID FetcherID; Status Status; ExitCode int; ErrorReason string }
type Target       struct{ ID FetcherID; BaseID FetcherID; Label string }
type TargetsMsg   struct{ Targets []Target }
type StallTickMsg struct{}
```

`TargetsMsg` is what the real runner emits at the very start of `Start()`
to tell the Run screen "the IDs you gave me expanded to *these* concrete
cards." For a plain `EVD-S3-ENC` selection on a single AWS profile, it's
1:1. For a multi-instance config (`AWS_REGION_2_FETCHERS=...`) it can
expand into `EVD-S3-ENC_region_2`, `EVD-S3-ENC_region_3`, etc. The screen
re-keys its per-card state by the targets it gets.

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
classifies results, writes per-fetcher log files, expands selections
into multi-instance targets, and writes `summary.json` at end-of-run.
It uses goroutines because subprocesses are blocking.

### Config — what gets injected at construction

```go
// internal/runner/exec.go
type Config struct {
    Profile                string
    Region                 string
    FetcherRepoRoot        string                       // path to evidence-fetchers checkout
    OutputRoot             string                       // per-run evidence dir
    EvidenceSetsCompatPath string                       // mirror evidence_sets.json into the repo
    Scripts                map[FetcherID]catalog.Script
    AuthChecker            AuthChecker                  // seam (real or fake)
    Environ                []string                     // child-process env (with secrets)
    MaxParallel            int                          // <1 → 1 (default; avoids tmp races)
}
```

Two fields worth calling out:

- **`AuthChecker`** is an interface (`internal/runner/awsflight.go`).
  `CLIAuthChecker{}` shells out to `aws sts get-caller-identity`. Tests
  inject a fake. `main.go` wraps it in `preflight.CachedAuthChecker` so
  the same on-disk pre-flight cache the Welcome screen wrote is reused
  during the run — no redundant `aws sts` calls per fetcher.
- **`Environ`** is the explicit child-process environment. We don't pass
  `os.Environ()` directly; we run it through `secrets.BuildEnviron(...)`
  first so keychain-stored secrets land in the subprocess without ever
  touching the parent process's environment. (See Part 10 for the
  Secrets package.)

### The lifecycle of one fetcher run

```
        Start(ids) ──► writeEvidenceSets ──► InstancesFromEnv (multi-inst expand)
            │                                      │
            ▼                                      ▼
   queue ◄── states populated ◄── targets ──► TargetsMsg → Run screen
            │
            ▼
       fillRunning  ──► spawn execute() goroutine ─┐
        (cap by                  │                 ├── pipeReader (stdout) ─┐
         MaxParallel)            ▼                 ├── pipeReader (stderr) ─┤
                       AWS preflight (memo)        │                        │
                                 │                 └── (each tee→ log file +├──► sender.Send(OutputMsg)
                                 ▼                                          │
                        cmd.Start() + wg.Wait() ◄─────────────────────────┘
                                 │
                                 ▼
                            cmd.Wait()
                                 │
                                 ▼
                       classify(deadline, cancel, exit, AWS post-flight)
                                 │
                                 ▼
                          FinishedMsg → screen
                                 │
                                 ▼
                          advance() — fillRunning again
                                 │
                                 ▼ (when all states terminal)
                       SummaryWriter.WriteSummary → summary.json
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

In `main.go`, the runner is constructed with
`preflight.CachedAuthChecker` instead of bare `CLIAuthChecker`, so the
result is *also* persisted to a JSON cache file
(`pre-flight-cache.json`) with a 5-minute TTL. The Welcome screen wrote
that file before the run started; reusing it avoids a second `aws sts`
shell-out at the top of the first AWS fetcher.

### Multi-instance expansion

`internal/runner/multiinstance.go` parses two env-var conventions
inherited from the upstream Python runner:

- `GITLAB_PROJECT_<N>_URL=...`, `GITLAB_PROJECT_<N>_API_ACCESS_TOKEN=...`,
  `GITLAB_PROJECT_<N>_FETCHERS=<csv-of-ids>` — fan out the listed
  fetchers across multiple GitLab projects.
- `AWS_REGION_<N>_REGION=...`, `AWS_REGION_<N>_PROFILE=...`,
  `AWS_REGION_<N>_FETCHERS=<csv>` — fan out across regions/profiles.

`InstancesFromEnv(ids, scripts, env)` returns a `[]Instance`. For a
plain selection without these vars, each instance is `Instance{ID: id,
BaseID: id}` (1:1). With them, one selected `EVD-FOO-BAR` may produce
`EVD-FOO-BAR_region_2`, `EVD-FOO-BAR_region_3`, etc. — each with its
own per-target output directory and its own `Env` overlay.

### Per-fetcher timeouts

`internal/runner/timeout.go` resolves the subprocess timeout in three
layers (highest precedence first):

1. `<UPPER_SCRIPT_KEY>_TIMEOUT` env var — per-fetcher override.
2. `FETCHER_TIMEOUT` env var — global default.
3. 300 seconds — hard-coded fallback.

Plus a special case: `ssllabs_tls_scan` is floored at 3600 seconds
(overridable via `SSLLABS_FETCHER_TIMEOUT`). Qualys SSL Labs polls
take minutes per host; the default 300s would always cut them off.

### summary.json at end-of-run

`internal/runner/summary.go` emits `summary.json` in the run's evidence
directory the moment all fetchers reach terminal state. The schema
matches what the upstream Python runner writes (`script_name`,
`evidence_directory`, per-result entries, error reasons), so the
existing Paramify uploader can consume our output without changes.

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

### Welcome — profile picker + AWS pre-flight

`internal/screens/welcome.go` shows the AWS profile list (loaded by
`preflight.LoadAWSProfiles` from `~/.aws/config`) plus a tools row
("aws ✓ jq ✓ python3 ✓ kubectl ✗ …" via `preflight.CheckTools`). Hitting
enter doesn't immediately advance — it kicks off an AWS credential check
through `preflight.Service.CheckAWS`, which uses the cached
`pre-flight-cache.json` if a recent successful check exists.

If credentials check out, Welcome emits `SelectedProfileMsg` and the
root model advances to Select. If they don't, the screen surfaces the
error inline; if it looks like an SSO expiry, pressing `o` shells out
to `aws sso login --profile <name>` (handled by Welcome's
`loginCmd`).

Welcome also exposes `s` → `OpenSecretsMsg`, opening the Secrets editor
without leaving the welcome flow.

```go
// internal/screens/welcome.go (sketch)
type WelcomeModel struct {
    profiles   []Profile
    tools      []preflight.ToolStatus
    credential *preflight.Service
    cursor     int
    // ...checking, ssoReady, status flags
}

type SelectedProfileMsg struct{ Profile Profile }
type OpenSecretsMsg     struct{}
```

Things worth noticing:

- **Type switch** (`switch msg := msg.(type)`). We don't know what kind
  of message we got; we case on the type. The new variable `msg` inside
  each case is typed as `tea.KeyMsg` (or whatever).
- **Closure-as-Cmd**: enter → `m.checkProfileCmd(p)` returns
  `func() tea.Msg { return profileCheckDoneMsg{...} }`. Bubble Tea runs
  it on a goroutine; the result message arrives in the next Update.
- **No global state.** The cursor and async flags are fields on the
  model.

### Secrets — the editor that's not on the linear path

`internal/screens/secrets.go` is reachable from any screen that knows how
to emit a `screens.OpenSecretsMsg` (Welcome via `s`, Select via `s`,
Review when it discovers the upload token is missing). When the operator
finishes, the screen emits `SecretsDoneMsg` and the root returns to
whichever screen sent them in.

Two construction modes:

- **Default mode**: `NewSecrets(keys, store)` shows a two-pane layout —
  a left source list (paramify pseudo-source plus every catalog source
  from `mock.Sources()`), a right key list with provenance ("set
  (keychain)" vs "set (env)").
- **Focused mode**: `NewSecretsWithOptions(keys, store,
  SecretsOptions{FocusKeys: [...], Prompt: "..."})` collapses the screen
  into a single section listing only the keys the caller cares about.
  Used by Review when it routes to Secrets to fix
  `PARAMIFY_UPLOAD_API_TOKEN` mid-upload.

Set values are written through `secrets.Store.Set`. With the default
`merged` backend (keychain primary + env fallback), writes go to the
OS keychain — env values remain read-only. Each fetcher subprocess gets
the merged view via `secrets.BuildEnviron`.

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

### Review — paged table + real Paramify upload

```go
// internal/screens/review.go
type ReviewModel struct {
    keys        app.KeyMap
    results     []RunResult
    evidenceDir string                  // shown when non-empty
    cursor      int
    offset      int                     // for paging
    phase       uploadPhase
    progress    progress.Model

    store           secrets.Store       // re-checked at u-press time
    paramifyFactory ParamifyFactory     // builds an uploader on demand
    uploadSummary   uploader.Summary
    uploadErr       error
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

#### Upload — when `u` is pressed

In demo mode the factory is `nil`, so `u` runs a scripted progress-bar
animation and that's it (the original "mock upload"). In live mode,
`main.go` plumbs a `ParamifyFactory` into the root model:

```go
paramifyFactory = func() (uploader.Uploader, error) {
    env, _ := secrets.BuildEnviron(os.Environ(), secretStore, secrets.RuntimeKeys())
    return uploader.NewPython(uploader.PythonConfig{
        FetcherRepoRoot: repoAbs,
        BaseURL:         firstEnvValue(env, secrets.KeyParamifyAPIBaseURL),
        Environ:         env,
    })
}
```

Two things to notice:

- The factory is invoked **at upload time**, not at Review construction.
  If the operator edits `PARAMIFY_API_BASE_URL` or
  `PARAMIFY_UPLOAD_API_TOKEN` in Secrets between the run finishing and
  pressing `u`, the upload picks up the new values.
- Before invoking the factory, Review re-reads the upload token from
  `secrets.Store`. If it's empty, Review emits
  `OpenSecretsForReviewMsg` — the root opens Secrets in *focused mode*
  for `PARAMIFY_UPLOAD_API_TOKEN` + `PARAMIFY_API_BASE_URL`, and
  returns to Review on `SecretsDoneMsg`.

The actual upload runs as a tea.Cmd that calls
`uploader.Uploader.ProcessEvidenceDir(ctx, evidenceDir)`. Today's
implementation (`uploader.PythonUploader`) shells out to
`evidence-fetchers/2-create-evidence-sets/paramify_pusher.py`. There's a
parallel native Go path in `internal/uploader/paramify.go` for a future
direct-HTTP swap.

---

## Part 10 — Support packages

These four packages don't fit cleanly into "runner" or "screens" — they
sit alongside both. Each is small (one or two files); together they
account for most of what changed when the prototype graduated from
"demo with a hard-coded Python uploader stub" to "actually uploads
evidence."

### `internal/secrets` — keys without a config file

Goal: let an operator set `PARAMIFY_UPLOAD_API_TOKEN`, `OKTA_API_TOKEN`,
etc. from inside the TUI without writing them to disk in plaintext.

```go
// internal/secrets/store.go
type Store interface {
    Get(key string) (value string, found bool, err error)
    Set(key, value string) error
    Delete(key string) error
    List() ([]string, error)
    Source() string                              // "keychain", "env", "merged"
    Writable() bool
    Locate(key string) (source string, found bool, err error)
    ParamifyUploadAPIToken() (string, error)
}
```

Three concrete implementations:

- `secrets.Env` — read-only view of `os.Environ()`. Exists so the
  `merged` backend can fall back to env vars without rewriting them.
- `secrets.Keychain` — wraps the OS keychain (macOS Keychain, Linux
  Secret Service, Windows Credential Manager via `go-keyring`). Set,
  Delete, List all work. Read-side falls through to OS APIs.
- `secrets.Merged` — `Primary` (keychain) layered over `Fallback`
  (env). Reads check Primary first; writes go to `Writer` (keychain by
  default). The UI's "set (keychain)" / "set (env)" provenance comes
  from `Locate(key)`.

`secrets.BuildEnviron(parentEnv, store, keys)` returns a child-process
environ slice with secret values overlaid. We use this to populate
`Config.Environ` for the real runner so secrets reach the fetcher
subprocess without touching the TUI's own `os.Environ`.

`secrets.ValidateKey` enforces an allowlist (defined in
`requirements.go`). Trying to `Set` an unknown key returns an error —
this prevents typos from quietly storing arbitrary process environment
in the OS keychain.

### `internal/output` — paths and the session log

`output.Home()` is the data root. Resolution order:

1. `PARAMIFY_FETCHER_HOME` env var (override)
2. `XDG_DATA_HOME/paramify-fetcher` (XDG spec)
3. `~/.local/share/paramify-fetcher` (fallback)

From there: `Home/evidence/<run-ts>/` for per-run evidence,
`Home/logs/session-<run-ts>.log` for the session log.
`output.RunTimestamp(time.Now())` produces the filesystem-safe
`2026-05-06T14-23-01Z` stem reused by both directories.

`output.SessionLog` is a concurrent-safe append-only log. The CLI opens
one at startup and `defer`s `Close()`. `output.SenderTap` wraps
`runner.Sender` so `StartedMsg` / `FinishedMsg` are mirrored into the
log file (note: we never log `OutputMsg` — those go to per-fetcher
`stdout.log` / `stderr.log` files inside the evidence dir).

### `internal/preflight` — credential checks the UI shares with the runner

```go
// internal/preflight/service.go
type Service struct {
    Checker runner.AuthChecker      // the actual `aws sts` shellout
    Login   LoginRunner             // optional: SSO repair flow
    Cache   string                  // path to JSON cache (e.g. pre-flight-cache.json)
    TTL     time.Duration           // default 5m
}
```

`Service.CheckAWS(ctx, profile, region)` returns a `Result{OK,
FromCache, SSOError, Err}`. If a recent successful check sits in the
cache, it returns immediately; otherwise it shells out, classifies the
error (SSO expired vs anything else), and writes the result back to
the cache. The Welcome screen calls this to gate continuation; the real
runner reuses the **same** cache via `preflight.CachedAuthChecker`
(which adapts `Service` to the runner's `AuthChecker` interface). One
auth check per profile/region per 5-minute window — not one per
fetcher.

`preflight.LoadAWSProfiles("")` parses `~/.aws/config` so Welcome can
present a real profile list without us hard-coding anything.
`preflight.CheckTools([...])` runs `which` for each named binary so
Welcome can warn the operator that, say, `python3` is missing before
Run fails 12 times in a row.

### `internal/uploader` — the Paramify push

```go
// internal/uploader/uploader.go
type Uploader interface {
    ProcessEvidenceDir(ctx context.Context, dir string) (Summary, error)
}
```

Two implementations:

- `uploader.PythonUploader` (`python.go`) — shells out to
  `evidence-fetchers/2-create-evidence-sets/paramify_pusher.py`. The
  pusher reads the per-run evidence directory, posts evidence sets to
  Paramify's REST API, and writes back receipts. We pass the upload
  token and base URL through the child-process environment so the
  Python script doesn't have to know about our `secrets.Store`.
- `uploader.ParamifyClient` (`paramify.go`) — a native Go HTTP client
  for the same API. Not currently wired into Review, but exists so we
  can drop the Python dependency once the contract tests pass on it.

The `uploader.Summary` returned to Review carries upload counts and
per-evidence-set status, which Review renders inline once the upload
finishes.

### `internal/evidence` — `evidence_sets.json`

`evidence.Render(selectedIDs, scripts)` builds the
`evidence_sets.json` document the Python pusher expects. The real
runner calls `evidence.Write(...)` at the start of `Start()` so the
file is in the per-run evidence dir before any fetcher runs (and is
optionally mirrored back into the fetcher repo at
`Config.EvidenceSetsCompatPath` for the legacy run path).

---

## Part 11 — Wiring

### The root model

```go
// internal/root/model.go
type Model struct {
    keys    app.KeyMap
    screen  Screen                  // Welcome / Secrets / Select / Run / Review
    runner  runner.Runner           // shared across sub-models that need it

    welcomeOpts     screens.WelcomeOptions
    evidenceDir     string
    paramifyFactory screens.ParamifyFactory   // nil → demo upload animation
    secrets         secrets.Store
    pendingReview   bool                       // true when Secrets was opened from Review
    secretBack      Screen                     // where to return after Secrets

    welcome screens.WelcomeModel
    sec     screens.SecretsModel
    sel     screens.SelectModel
    run     screens.RunModel
    review  screens.ReviewModel
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Global escape hatches: ctrl+c, q (most screens), ?, etc.
    // ...

    switch msg := msg.(type) {
    case screens.OpenSecretsMsg:
        m.secretBack = m.screen
        m.sec = screens.NewSecrets(m.keys, m.secrets).Resize(m.width, m.height)
        m.screen = ScreenSecrets
        return m, m.sec.Init()

    case screens.SelectedProfileMsg:
        m.profile, m.region = msg.Profile.Name, msg.Profile.Region
        if cfg, ok := m.runner.(runner.ProfileConfigurer); ok {
            cfg.ConfigureProfile(msg.Profile.Name, msg.Profile.Region)
        }
        m.sel = screens.NewSelect(m.keys, m.profile).Resize(m.width, m.height)
        m.screen = ScreenSelect
        return m, m.sel.Init()

    case screens.SelectionConfirmedMsg:
        m.run = screens.NewRun(m.keys, m.profile, msg.IDs, m.runner).Resize(m.width, m.height)
        m.screen = ScreenRun
        return m, m.run.Init()

    case screens.RunCompleteMsg:
        rev := screens.NewReview(m.keys, m.profile, msg.Results).
            WithEvidenceDir(m.evidenceDir)
        if m.paramifyFactory != nil {
            rev = rev.WithParamifyUpload(m.secrets, m.paramifyFactory)
        }
        m.review = rev.Resize(m.width, m.height)
        m.screen = ScreenReview
        return m, m.review.Init()

    case screens.OpenSecretsForReviewMsg:
        // Review couldn't upload — token missing. Open Secrets in focused
        // mode and return here when done.
        m.pendingReview = true
        m.secretBack = ScreenReview
        m.sec = screens.NewSecretsWithOptions(m.keys, m.secrets, screens.SecretsOptions{
            FocusKeys: []string{secrets.KeyParamifyUploadAPIToken, secrets.KeyParamifyAPIBaseURL},
            Prompt:    "Paramify upload needs PARAMIFY_UPLOAD_API_TOKEN ...",
        }).Resize(m.width, m.height)
        m.screen = ScreenSecrets
        return m, m.sec.Init()

    case screens.SecretsDoneMsg:
        // Return to whichever screen sent us here.
        if m.pendingReview { m.pendingReview = false; m.screen = ScreenReview; return m, nil }
        // ...else: ScreenSelect / ScreenRun / Welcome
    }

    // Otherwise: route to the active screen.
    switch m.screen {
    case ScreenWelcome: m.welcome, cmd = m.welcome.Update(msg)
    case ScreenSecrets: m.sec, cmd     = m.sec.Update(msg)
    case ScreenSelect:  m.sel, cmd     = m.sel.Update(msg)
    case ScreenRun:     m.run, cmd     = m.run.Update(msg)
    case ScreenReview:  m.review, cmd  = m.review.Update(msg)
    }
}
```

The root does three things:

1. **Catches transition messages** (the `Selected*Msg` / `*ConfirmedMsg` /
   `*CompleteMsg` types) and switches the active screen.
2. **Routes Secrets detours.** `OpenSecretsMsg` and
   `OpenSecretsForReviewMsg` push the current screen onto a one-deep
   "back" slot; `SecretsDoneMsg` pops it. This is what makes Secrets
   reachable from any other screen without each screen having to know
   about it.
3. **Forwards everything else** to the currently active sub-model.

The root is also where `runner.ProfileConfigurer` is used: when the
operator picks a profile, the type assertion either succeeds (real
runner — set the AWS profile/region) or fails harmlessly (mock runner
— ignore).

This is the **finite state machine** pattern at the screen level, on
top of the FSM pattern at the runner level. Composable, easy to test —
each screen can be exercised in isolation.

### `main.go` — flag parsing and runner wiring

```go
// main.go (sketch)
func main() {
    demo            := flag.Bool("demo", true, "use the deterministic mock runner")
    catalogPath     := flag.String("catalog", "", "override the embedded catalog (dev)")
    profile         := flag.String("profile", "", "AWS profile (real runner)")
    region          := flag.String("region", "", "AWS region (real runner)")
    repoRoot        := flag.String("fetcher-repo-root", "", "path to evidence-fetchers checkout")
    outputRoot      := flag.String("output-root", "", "explicit per-run evidence dir (overrides XDG)")
    fetcherParallel := flag.Int("fetcher-parallel", 1, "max concurrent fetcher subprocesses")
    secretsBackend  := flag.String("secrets-backend", "merged", "merged|keychain|env")
    flag.Parse()

    if *catalogPath != "" { mock.SetCatalogOverride(*catalogPath) }
    if err := mock.EnsureCatalog(); err != nil { die(2, "catalog error: %v", err) }

    runTS := output.RunTimestamp(time.Now())
    sessionLog, _ := output.OpenSessionLog(runTS)
    defer sessionLog.Close()

    secretStore, _ := buildSecretsStore(*secretsBackend)
    runtimeEnv, _  := secrets.BuildEnviron(os.Environ(), secretStore, secrets.RuntimeKeys())

    var (
        r               runner.Runner
        welcomeOpts     screens.WelcomeOptions
        evidenceDir     string
        paramifyFactory screens.ParamifyFactory
    )
    if *demo {
        r = mock.NewMockRunner(mock.Catalog())
    } else {
        var repoAbs string
        r, evidenceDir, repoAbs = buildRealRunner(*profile, *region, *repoRoot,
            *catalogPath, *outputRoot, *fetcherParallel, runTS, runtimeEnv)
        welcomeOpts = realWelcomeOptions(*profile, *region)   // profiles + tools + cred service
        paramifyFactory = func() (uploader.Uploader, error) {
            env, _ := secrets.BuildEnviron(os.Environ(), secretStore, secrets.RuntimeKeys())
            return uploader.NewPython(uploader.PythonConfig{
                FetcherRepoRoot: repoAbs,
                BaseURL:         firstEnvValue(env, secrets.KeyParamifyAPIBaseURL),
                Environ:         env,
            })
        }
    }

    rootModel := root.NewWithOptions(r, root.Options{
        Welcome:         welcomeOpts,
        EvidenceDir:     evidenceDir,
        ParamifyFactory: paramifyFactory,
        Secrets:         secretStore,
    })
    p := tea.NewProgram(rootModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
    r.Bind(output.SenderTap{Inner: p, Log: sessionLog})   // ← log Started/Finished as they pass
    if _, err := p.Run(); err != nil { ... }
}
```

The order matters:

1. **Parse flags first.** Don't open the alt-screen if the user passed
   `--help`; let `flag.Parse` handle that.
2. **Validate catalog and open the session log before the TUI.** A
   catalog error would be invisible inside the alt-screen.
3. **Build the secrets store and the merged child-process environ.**
   Both the real runner (for AWS calls) and the Paramify factory need
   these.
4. **Pick a runner.** Just an interface variable. Real-mode also
   produces a `WelcomeOptions` (profiles + tool checks + cached cred
   service) and a `ParamifyFactory`.
5. **Wire it into the program.** `tea.NewProgram` creates the program;
   `r.Bind(SenderTap{Inner: p, Log: sessionLog})` gives the runner a
   way to push messages from goroutines, with terminal-state messages
   mirrored into the session log.
6. **Run.** `p.Run` blocks until the program exits.

The `--demo` flag defaults to `true` because the prototype's most
common use is the demo. To run for real you'd pass
`--demo=false --fetcher-repo-root=/path/to/evidence-fetchers
[--profile=... --region=...]`.

Most of the assembly happens in two helpers:

- `buildRealRunner(...)` validates `--fetcher-repo-root`, loads the
  catalog (embedded or `--catalog` override), resolves the per-run
  evidence directory, and constructs `runner.NewReal(runner.Config{...})`
  with `preflight.CachedAuthChecker` plumbed in for shared cred caching.
- `buildSecretsStore(backend)` returns `secrets.Env`,
  `secrets.Keychain`, or `secrets.Merged{Primary: keychain, Fallback:
  env, Writer: keychain}`. The default `merged` is what the README
  documents.

---

## Part 12 — Why we did it this way

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

The prototype only has a handful of top-level flags. `flag` from the
standard library handles that in a few lines. Adding a third-party CLI
framework would dwarf the actual feature it supports. We can adopt
cobra later if subcommands appear (`paramify-fetcher run`,
`paramify-fetcher list-presets`, etc.).

This is a recurring theme: **prefer the standard library** when it's
sufficient. Every dependency is a vector for breakage on someone
else's release schedule.

### Q: Why a secrets package instead of just reading `os.Getenv`?

Two reasons:

- **The TUI needs to *write* secrets**, not just read them. Operators
  set `PARAMIFY_UPLOAD_API_TOKEN` from the Secrets screen. Writing to
  `os.Environ` only affects the parent process; we want the value to
  persist across runs without touching shell rc files. The OS keychain
  is the right place for that.
- **We don't want secrets in the parent process's environment at all
  if we can help it.** `secrets.BuildEnviron` constructs a child-only
  environ slice. The TUI process itself doesn't need to read
  `OKTA_API_TOKEN`; the fetcher subprocess does. Keeping the secret
  out of `os.Environ` reduces the chance of it leaking into a crash
  dump or a logging library that snapshots the environment.

The `Store` interface gives us one seam: tests use
`secrets.NewMemory()`, headless CI uses `secrets.Env`, interactive
operators use `secrets.Merged{Primary: keychain, Fallback: env}`.

### Q: Why does the uploader shell out to Python?

The Python `paramify_pusher.py` already exists and already works
against the production Paramify API. Reimplementing it in Go would be
a rewrite for no immediate user-visible win, and would mean we have
two implementations to keep in sync with API changes.

`internal/uploader/paramify.go` is a Go-native HTTP client we can swap
in once its contract tests cover everything the Python pusher does. At
that point we drop `python3` as a runtime dependency for upload (live
fetcher runs still need it, though).

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

## Part 13 — Reading order

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
  list in that screen's `View`. Add a row to `helpSections()` in
  `internal/root/model.go` so `?` describes it.

- **"Understand how runs work end-to-end"** — read in this order:
  1. `internal/runner/runner.go` (the interface)
  2. `internal/runner/messages.go` (the public message types)
  3. `internal/screens/run.go` (the consumer)
  4. `internal/mock/runner_adapter.go` (one implementation)
  5. `internal/runner/real.go` (the other implementation)
  6. `internal/runner/multiinstance.go` (target expansion)
  7. `internal/runner/summary.go` (end-of-run output)

- **"Change how the catalog is loaded"** —
  `internal/catalog/{schema,loader,embedded}.go`. The decoder is in
  `schema.go`; the validation is in `loader.go`; the embed directive
  is in `embedded.go`.

- **"Add a new behavior to the mock (a new failure mode)"** — add a
  constant to the `BehaviorKind` enum in `internal/mock/fetchers.go`,
  add a case to `Build` in `internal/mock/runner.go`, optionally add
  an entry to `behaviorOverrides` to assign it to a specific fetcher.

- **"Add a new secret key (e.g., `SLACK_BOT_TOKEN`)"** — add a `Key…`
  constant in `internal/secrets/store.go`, add a row to the
  `requirements.go` table so it surfaces under the right source on
  the Secrets screen, and `secrets.RuntimeKeys()` will pick it up
  automatically (the child-process environ pulls every key in
  `AllSecretKeys()`).

- **"Change how the upload works"** — start with
  `internal/uploader/uploader.go` (the interface), then
  `internal/uploader/python.go` for the current shellout path. The
  Go-native HTTP path lives in `internal/uploader/paramify.go` and is
  exercised by `mvp_contract_test.go`.

- **"Wire the real runner into a deployment"** — read `main.go`'s
  `buildRealRunner`, then `internal/runner/exec.go`, then
  `internal/runner/real.go`. `internal/preflight/service.go` covers
  the cached AWS auth check.

- **"Move evidence/log paths to a different location"** —
  `internal/output/paths.go`. Override via
  `PARAMIFY_FETCHER_HOME` or `--output-root`.

- **"Write a test"** — start with `internal/root/smoke_test.go` for
  the shape, then look at how it constructs a runner and feeds
  messages. Heavier examples: `internal/runner/real_test.go` (real
  runner with a fake AuthChecker), `internal/screens/secrets_test.go`
  (secrets screen with `secrets.NewMemory()`).

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
| **Sender** | Smallest interface that lets a goroutine push messages into Bubble Tea's event loop. Implemented by `*tea.Program`; wrapped by `output.SenderTap` to mirror Started/Finished into the session log. |
| **SessionLog** | Append-only `Home/logs/session-<run-ts>.log` for support diagnostics. Captures Started/Finished — never `OutputMsg` content. |
| **Target** | One concrete card the Run screen renders. For non-multi-instance, `Target.ID == BaseID == FetcherID`. For multi-instance, `BaseID` is the catalog id; `ID` is per-instance. |
| **Profile** | An AWS profile (from `~/.aws/config`) the operator picks on Welcome. The real runner re-uses it for `aws sts` and `--profile` subprocess args. |
| **Pre-flight cache** | `pre-flight-cache.json` written by `preflight.Service`. 5-minute TTL. Both the Welcome screen and the real runner consume the same cache. |
| **Secrets backend** | One of `merged`, `keychain`, `env`. Selected at startup with `--secrets-backend`. |
| **Paramify upload** | The `u` action on Review. Today shells out to `evidence-fetchers/2-create-evidence-sets/paramify_pusher.py`; will eventually run through the native Go client in `internal/uploader/paramify.go`. |
| **Preset** | (Future) A saved, named selection of fetchers — "FedRAMP Low", "Customer ACME". Stored as TOML. |
