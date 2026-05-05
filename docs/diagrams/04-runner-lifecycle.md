# 04. Real runner lifecycle

> **STATUS:** stub — diagram body not yet drawn.

## Question this diagram answers

What happens, in order, between `Start` and the final status `Msg` for
one real fetcher?

This is a **sequence diagram**, not a state machine. Diagram 03 covers
the static shape; this one covers the time axis.

## Code of record

- [`internal/runner/real.go`](../../internal/runner/real.go) — the
  `Start` / queue / execute path.
- [`internal/runner/exec.go`](../../internal/runner/exec.go) — the
  `exec.Cmd` builder contract.
- [`internal/runner/awsflight.go`](../../internal/runner/awsflight.go) —
  pre-flight and post-flight AWS credential checks.
- [`internal/runner/timeout.go`](../../internal/runner/timeout.go) —
  per-fetcher timeout escalation.
- [`internal/runner/multiinstance.go`](../../internal/runner/multiinstance.go)
  — env-namespaced fan-out across multiple instances.
- [`internal/runner/messages.go`](../../internal/runner/messages.go) —
  the messages emitted along the way.

## Update when

- The order of pre-flight / execute / post-flight changes.
- Stdout/stderr piping or line classification logic changes.
- Timeout / cancellation semantics change.
- The shape of the "finished" message changes.

## Operator checklist

- What runs before the subprocess starts?
- How do output lines get classified into "ok / warn / error"?
- What happens when the subprocess hangs vs. crashes vs. exits with
  a non-zero code?
- What happens when the user presses cancel mid-run?
- What runs after the subprocess exits?

## Diagram

```eraser
// Eraser DSL goes here. This is a sequence diagram.
// Lifelines: RunModel, Runner, Goroutine, Subprocess, Sender
// Sequence sketch:
//   RunModel -> Runner: Start(targets)
//   Runner   -> Goroutine: spawn (per target)
//   Goroutine -> Subprocess: exec.Cmd
//   Subprocess -> Goroutine: stdout/stderr lines
//   Goroutine -> Sender: OutputMsg(s)
//   Subprocess -> Goroutine: exit
//   Goroutine -> Sender: FinishedMsg
//   Sender   -> RunModel: tea.Msg
```
