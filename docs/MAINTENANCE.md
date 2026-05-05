# Diagram maintenance rule

Diagrams drift when nobody owns the moment of update. This page makes the
moment mechanical: **if your PR touches one of the trigger areas below,
update the corresponding diagram in the same PR**. Reviewers should
reject PRs that change a trigger area without touching the diagram (or
without an explicit "diagram unchanged because ..." note).

## Trigger map

| If you change ... | Update this diagram |
| --- | --- |
| Screen routing or `*Msg` types in [`internal/root/model.go`](../internal/root/model.go) or new `OpenSomethingMsg`/`SomethingDoneMsg` types in [`internal/screens/`](../internal/screens/) | [`diagrams/01-router-fsm.md`](diagrams/01-router-fsm.md) |
| Required keys, backends, or injection logic in [`internal/secrets/`](../internal/secrets/) — including `requirements.go`, `merged.go`, `env.go`, `keychain.go`, `memory.go` | [`diagrams/02-secrets-auth.md`](diagrams/02-secrets-auth.md) |
| The `runner.Runner` interface, `Sender`, status classification, or the Mock/Real split in [`internal/runner/`](../internal/runner/) and [`internal/mock/`](../internal/mock/) | [`diagrams/03-runner-architecture.md`](diagrams/03-runner-architecture.md) |
| Real-runner subprocess lifecycle: `Start`, `exec.go`, `awsflight.go`, `timeout.go`, pipe handling, `multiinstance.go`, or finished-message shape in [`internal/runner/`](../internal/runner/) | [`diagrams/04-runner-lifecycle.md`](diagrams/04-runner-lifecycle.md) |
| Artifact paths, log file layout, evidence-set bundling, or uploader payload in [`internal/output/`](../internal/output/), [`internal/evidence/`](../internal/evidence/), or [`internal/uploader/`](../internal/uploader/) | [`diagrams/05-data-flow.md`](diagrams/05-data-flow.md) |
| New cross-package import that crosses an existing seam, or a new top-level `internal/` package | [`diagrams/06-package-deps.md`](diagrams/06-package-deps.md) (if present) and/or a new ADR |

## Diagram file template

Every diagram file under [`diagrams/`](diagrams/) follows the same shape
so reviewers know what to look for:

1. **Title** — `# NN. Short name`
2. **Question this diagram answers** — one sentence. If you can't write
   one, the diagram is doing two jobs and should be split.
3. **Code of record** — markdown links to the file(s) the diagram
   tracks. This is the source of truth; the diagram is a view.
4. **Update when** — a short list mirroring this page's row for the
   diagram, so the trigger is visible from the diagram itself.
5. **Operator checklist** — 3-5 questions the diagram should be able to
   answer once filled in. Examples: "what happens when AWS creds expire
   mid-session?", "where do secrets enter the subprocess env?".
6. **Diagram** — a fenced ` ```eraser ` block. May be empty in the
   skeleton phase; otherwise contains the Eraser DSL.

## When *not* to update a diagram

Not every PR needs a diagram update. Skip with a one-line note in the
PR description if:

- The change is internal to a package and doesn't cross a seam.
- The change is a refactor that preserves the existing shape.
- The change is a bug fix that doesn't change the contract.

If you're unsure, update the diagram. Over-updating is cheap; drift is
expensive.
