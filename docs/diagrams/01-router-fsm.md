# 01. Router FSM

> **STATUS:** stub — diagram body not yet drawn.

## Question this diagram answers

How does the TUI move between screens, and which `Msg` types drive each
transition?

## Code of record

- [`internal/root/model.go`](../../internal/root/model.go) — the
  `Model.Update` switch is the actual state machine.
- [`internal/screens/welcome.go`](../../internal/screens/welcome.go) —
  emits `SelectedProfileMsg`, `OpenSecretsMsg`.
- [`internal/screens/secrets.go`](../../internal/screens/secrets.go) —
  emits `SecretsDoneMsg`.
- [`internal/screens/select.go`](../../internal/screens/select.go) —
  emits `SelectionConfirmedMsg`, `OpenSecretsMsg`.
- [`internal/screens/run.go`](../../internal/screens/run.go) — emits
  `RunCompleteMsg`.
- [`internal/screens/review.go`](../../internal/screens/review.go) —
  emits `OpenSecretsForReviewMsg`, `RestartMsg`, `QuitMsg`.

## Update when

- New screen is added to the `Screen` enum in
  [`internal/root/model.go`](../../internal/root/model.go).
- A new `*Msg` transition type is introduced or an existing one changes
  meaning.
- Re-entry / "return path" logic changes (the
  `pendingRun` / `pendingReview` / `secretBack` fields).

## Operator checklist

When this diagram is filled in it should make these answerable at a
glance:

- What is the happy path from launch to "evidence uploaded"?
- How does Secrets get entered, and where does it return to in each
  case?
- What happens if a Selection includes a fetcher whose required secret
  is missing?
- What happens if `PARAMIFY_UPLOAD_API_TOKEN` is missing at upload time?
- How does an operator restart vs. quit?

## Diagram

```eraser
// Eraser DSL goes here.
// Boxes: Welcome, Secrets, Select, Run, Review.
// Arrows labeled with the *Msg types listed in "Code of record" above.
```
