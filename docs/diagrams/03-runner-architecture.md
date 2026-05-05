# 03. Runner architecture

> **STATUS:** stub — diagram body not yet drawn.

## Question this diagram answers

What is the seam between Bubble Tea's pure update loop and the
goroutine-driven runners?

## Code of record

- [`internal/runner/runner.go`](../../internal/runner/runner.go) — the
  `Runner` interface every implementation satisfies.
- [`internal/runner/sender.go`](../../internal/runner/sender.go) — the
  `Sender` bridge that lets goroutines emit `tea.Msg`s back to the
  program.
- [`internal/runner/messages.go`](../../internal/runner/messages.go) —
  the public message types screens react to.
- [`internal/runner/types.go`](../../internal/runner/types.go) —
  `FetcherID`, `Status`, `Target`.
- [`internal/runner/real.go`](../../internal/runner/real.go) — the
  production runner.
- [`internal/mock/runner.go`](../../internal/mock/runner.go) and
  [`runner_adapter.go`](../../internal/mock/runner_adapter.go) — the
  mock runner used in demo mode and tests.

## Update when

- A method is added to or removed from the `Runner` interface.
- A new public message type is added in `messages.go`.
- A new runner implementation is introduced.
- The `Sender` contract changes (e.g., new ways for goroutines to push
  messages into the Bubble Tea program).

## Operator checklist

- Which side of the diagram is allowed to spawn goroutines?
- How does a screen ask the runner to start, cancel, or finish work
  without breaking `Update` purity?
- How do output lines and finished signals get back to the UI?
- What's the same between Mock and Real, and what's different?

## Diagram

```eraser
// Eraser DSL goes here.
// Suggested shape:
//   center: Runner interface
//   left:   Mock impl (synchronous, scripted)
//   right:  Real impl (subprocesses, goroutines)
//   bottom: Sender bridge -> tea.Program
// Annotate the goroutine boundary clearly.
```
