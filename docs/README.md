# docs/

Architecture and design documentation for `paramify-fetcher`. This folder is
the canonical reference for **how the system is shaped**. Code-tour prose
lives in [`../WALKTHROUGH.md`](../WALKTHROUGH.md); product framing lives in
[`../DESIGN.md`](../DESIGN.md); usage lives in [`../README.md`](../README.md).

## How to read this folder

- **Diagrams** ([`diagrams/`](diagrams/)) are the source of truth for the
  shape of the system: screens, runners, secrets, data flow. When the code
  and a diagram disagree, the diagram is wrong — fix it.
- **[`DESIGN-TRUTHS.md`](DESIGN-TRUTHS.md)** is the single page to re-read
  when asking "are we still building the right thing?". User outcomes,
  non-functional requirements, constraints, and key seams.
- **[`adr/`](adr/)** holds Architecture Decision Records — short,
  numbered notes explaining *why* we made a non-obvious choice we will
  forget the reason for in three months.
- **[`MAINTENANCE.md`](MAINTENANCE.md)** is the mechanical rule for
  keeping diagrams in sync with code: "if you change X, update diagram Y".
- **[`../WALKTHROUGH.md`](../WALKTHROUGH.md)** is a code tour for new
  developers. It links *into* this folder for architecture; it does not
  duplicate the diagrams.

## Diagram index

Each diagram answers exactly one question. If a question feels too big for
one diagram, that's a signal to split — not to add a seventh.

- [`diagrams/01-router-fsm.md`](diagrams/01-router-fsm.md) — How does the
  TUI move between screens, and which `Msg` types drive each transition?
- [`diagrams/02-secrets-auth.md`](diagrams/02-secrets-auth.md) — Where do
  secrets come from, when are they required, and how do they reach the
  fetcher subprocess?
- [`diagrams/03-runner-architecture.md`](diagrams/03-runner-architecture.md)
  — What is the seam between Bubble Tea's pure update loop and the
  goroutine-driven runners?
- [`diagrams/04-runner-lifecycle.md`](diagrams/04-runner-lifecycle.md) —
  What happens, in order, between `Start` and the final status `Msg` for
  one real fetcher?
- [`diagrams/05-data-flow.md`](diagrams/05-data-flow.md) — What artifacts
  exist at each stage, from embedded catalog to uploader payload?
- [`diagrams/06-package-deps.md`](diagrams/06-package-deps.md) —
  *Optional.* Which packages depend on which, and where are the seams?

## Conventions

- One diagram per file, named `NN-short-name.md`.
- Each diagram file uses the template described in
  [`MAINTENANCE.md`](MAINTENANCE.md): title, question, code of record,
  update triggers, operator checklist, then the diagram source.
- Diagrams are committed as Eraser DSL inside a fenced ` ```eraser ` block
  so PR diffs are reviewable. An exported `<name>.svg` next to the source
  is optional but recommended for GitHub previews.
- The Eraser hosted editor is fine to use — paste from the `.md`, edit,
  paste back. The committed `.md` is authoritative.
