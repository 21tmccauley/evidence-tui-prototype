# Design truths

The single page to re-read when asking *"are we still building the right
thing?"*. Each section is a list of statements we currently believe are
true. When one of these stops being true, that's a real event — write an
[ADR](adr/) explaining what changed and why.

Keep entries short. If a bullet needs a paragraph, it probably belongs in
an ADR or a diagram, not here.

---

## User outcomes

What "done" looks like for the operator running this tool. Concrete and
observable, not feature lists.

### Personas

The same workflow serves two audiences:

- **Customer operator** — a compliance, security, or IT person at the
  Paramify customer running the tool against their own environment.
- **Paramify employee** — running collection on behalf of a customer
  during onboarding.

Today they share one workflow; if they ever diverge, that's an ADR.

### Headline outcome

> The operator launches the TUI, picks the evidence they want collected,
> the scans run, and the resulting evidence is either exported locally
> or uploaded to Paramify. Then they're done.

### The intended flow

1. **Launch the TUI** (single binary; live mode requires
   `--fetcher-repo-root`).
2. **Configure secrets** — open Secrets from Welcome (or Select) at any
   time. The Secrets screen lists every catalog source plus a pinned
   Paramify entry; the operator sets whichever keys their selection
   needs. The TUI does not decide what's required for a given run.
3. **Select fetchers.** The selection is saveable as a named **preset**
   that records both the chosen fetchers and the secrets they require,
   so future runs can be a one-step "load preset → run". *(Preset
   persistence is the documented intent — see Open questions for its
   current implementation status.)*
4. **Run starts immediately on confirmed selection.** Missing keys
   surface as fetcher failures (`runner.Real` fail-fasts AWS preflight
   only; everything else fails inside the script with its own error).
   The operator opens Secrets, fixes the key, retries the
   failed card.
5. **Scans run** in parallel under the runner.
6. **Review** — the operator inspects results and chooses one of:
   - **Export locally** — the evidence directory on disk is the
     deliverable.
   - **Upload to Paramify** — the in-app uploader pushes the evidence
     directory to the Paramify API. This step *is* gated:
     `PARAMIFY_UPLOAD_API_TOKEN` must be set, and Review routes the
     operator through Secrets if it isn't.
7. **Done.**

### Primary use cycle

This tool is for **manual, ad-hoc evidence collection by a human
operator**. It is explicitly the right answer for:

- **Onboarding** — a customer's first time producing evidence for a
  Paramify program.
- **Operators who don't want to stand up automated infrastructure** to
  run the fetcher scripts.
- **Operators whose selection changes between runs** (e.g.,
  scope changes, exploratory collection, ad-hoc spot-checks).

### Explicit non-priorities (today)

These are real constraints on scope. They are not non-goals
*forever* — flag with an ADR if they become priorities — but right
now we deliberately do not invest in:

- **Resumable / partial runs.** A run is a single session.
- **Long-term run history or audit trails inside the TUI.**
- **Replacing the `evidence-fetchers/` repo or its scripts.** This tool
  drives those scripts; it does not subsume them.
- **Managing AWS credentials for the user.** SSO login is delegated to
  the system `aws` CLI.

## Non-functional requirements

The properties the system must hold across releases. Each one is
testable or at least observable; if a bullet is neither, it's a wish,
not an NFR.

- **Offline demo mode is hermetic.** With `--demo=true` (the default),
  the binary opens no network sockets and runs no external programs. A
  contributor who breaks this should fail a test, not a customer.
- **One statically-linked binary is the deliverable.** No `pip install`,
  no `npm`, no companion daemons. Fetcher subprocesses are free to need
  whatever they need; the TUI is not.
- **Secret hygiene is a tested invariant.** Secret values must never
  appear in stdout, the session log file, screenshots, or any error
  message. Today this is a code-review rule; we want it backed by an
  output-snapshot test that fails if a known secret value leaks into
  any rendered string or log line. (See
  [`DESIGN.md`](../DESIGN.md) for the snapshot-test pattern.)
- **TUI stays responsive at the realistic upper bound.** A run of ~100
  fetchers in parallel must not block the Bubble Tea event loop, drop
  output lines, or freeze the cursor. This is the target scale, not a
  hard SLO; if we exceed 100, that's an ADR.
- **Fetcher-script independence.** The TUI works against any
  `evidence-fetchers/` checkout that satisfies the documented script
  contract (exec interface, output shape). We do not pin to a specific
  fetcher commit; coupling lives behind
  [`runner.exec`](../internal/runner/exec.go) and the catalog schema.
- **Mac and Linux are first-class targets.** Windows is out of scope
  for now — note that the keychain backend
  ([`internal/secrets/keychain.go`](../internal/secrets/keychain.go))
  differs across OSes, which is the main portability cost we accept.

### Candidates not yet promoted

These are real-but-not-yet. Listed here so contributors don't
accidentally regress them, and so we know what an NFR-promotion ADR
might cover next:

- **Headless / CI-friendly collection.** Today the TUI requires a TTY.
  Env-based secret injection already works
  ([`internal/secrets/env.go`](../internal/secrets/env.go)), but the
  whole flow has no non-interactive path. Nice to have, not required.

## Constraints

The architectural rules we've chosen to live by. Breaking one of these
is allowed, but only via an ADR. Each bullet is grounded in code; the
file links are the source of truth.

- **Bubble Tea `Update` is pure.** No I/O, no goroutines spawned inline.
  All side effects go through `tea.Cmd`. See
  [`internal/root/model.go`](../internal/root/model.go) and
  [`../WALKTHROUGH.md`](../WALKTHROUGH.md) Part 2.
- **All runner concurrency lives behind `runner.Runner`.** Screens never
  spawn goroutines. The real runner's goroutines reach the UI only by
  calling `Sender.Send(msg)`.
  ([`internal/runner/runner.go`](../internal/runner/runner.go),
  [`internal/runner/sender.go`](../internal/runner/sender.go))
- **The fetcher catalog is `//go:embed`-ed at build time.** The
  `--catalog` flag exists only as a development override, not a runtime
  configuration surface.
  ([`internal/catalog/embedded.go`](../internal/catalog/embedded.go))
- **Catalog IDs match `EVD-<UPPER>(-<UPPER>)+`.** Duplicate IDs are a
  load-time error, not a warning.
  ([`internal/catalog/loader.go`](../internal/catalog/loader.go))
- **Secret keys are allowlisted.** Stores reject unknown keys via
  `secrets.ValidateKey` so accidental writes can't pollute the OS
  keychain with unrelated environment variables.
  ([`internal/secrets/store.go`](../internal/secrets/store.go))
- **Read-only stores fail closed.** The `Env` backend returns
  `ErrReadOnly` from `Set`/`Delete` instead of silently accepting
  values.
  ([`internal/secrets/env.go`](../internal/secrets/env.go))
- **`internal/` is the privacy boundary.** Anything under it is
  unimportable from outside this Go module — refactor freely.
- **The deliverable is a single statically-linked Go binary.** Runtime
  Python is acceptable inside fetcher *subprocesses* (which the user
  supplies via `--fetcher-repo-root`), but never as a runtime dependency
  of the TUI itself.
- **Demo mode is offline.** `--demo=true` (the default) makes no AWS
  calls and runs no external scripts; it uses the deterministic mock
  runner. ([`main.go`](../main.go),
  [`internal/mock/runner.go`](../internal/mock/runner.go))

## Key seams / interfaces

The contracts that let us swap or extend implementations without
rewriting screens. If you find yourself reaching across one of these
seams, stop and reconsider.

- **`runner.Runner`** —
  [`internal/runner/runner.go`](../internal/runner/runner.go). Methods:
  `Init`, `Update`, `Start`, `Cancel`, `Retry`, `Bind`. Implementations:
  the production runner ([`runner.Real`](../internal/runner/real.go))
  and the demo runner
  ([`mock.RunnerAdapter`](../internal/mock/runner_adapter.go)).
  Runners that need credentials chosen inside the TUI implement the
  optional `runner.ProfileConfigurer`.
- **`runner.Sender`** —
  [`internal/runner/sender.go`](../internal/runner/sender.go). Single
  method `Send(msg tea.Msg)`, goroutine-safe. `*tea.Program` is the
  canonical implementation; [`output.SenderTap`](../internal/output/session.go)
  wraps it to tee runner messages to the session log.
- **`secrets.Store`** —
  [`internal/secrets/store.go`](../internal/secrets/store.go). Methods:
  `Get`, `Set`, `Delete`, `List`, `Source`, `Writable`, `Locate`,
  `ParamifyUploadAPIToken`. Backends:
  [`Env`](../internal/secrets/env.go) (read-only),
  [`Keychain`](../internal/secrets/keychain.go),
  [`Memory`](../internal/secrets/memory.go) (test-only), and
  [`Merged`](../internal/secrets/merged.go) (primary + fallback +
  writer). The CLI selects one at startup via `--secrets-backend`.
- **`uploader.Uploader`** —
  [`internal/uploader/uploader.go`](../internal/uploader/uploader.go).
  Single method `ProcessEvidenceDir(ctx, dir) (Summary, error)`.
  Production impl:
  [`uploader.Python`](../internal/uploader/python.go). Review depends on
  a factory function, not a concrete client, so a token edited
  mid-session takes effect on the next upload without restarting the
  TUI.
- **Catalog loader** —
  [`internal/catalog/loader.go`](../internal/catalog/loader.go). Entry
  point `catalog.Load(io.Reader) (*Catalog, []Script, error)`. The JSON
  schema in
  [`internal/catalog/schema.go`](../internal/catalog/schema.go) is the
  contract between this repo and `evidence-fetchers/`.

## Open questions

A running list. Date entries; remove or promote to an ADR when resolved.

- **2026-05-05: Preset persistence is documented but not implemented.**
  [`DESIGN.md`](../DESIGN.md) Part 6 specifies named, shareable preset
  TOML files; today no `.go` file references presets and Welcome has no
  preset picker. Decide: ship presets next, or reframe the intended-flow
  bullet to match the current ephemeral selection.
- **2026-05-05: Secret-hygiene NFR has no enforcing test.** The NFR
  above is asserted but not checked. Ship a snapshot test that runs a
  representative session with known sentinel values and fails if any of
  them appear in rendered output, the session log, or error strings.
