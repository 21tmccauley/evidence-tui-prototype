# 02. Secrets and auth flow

> **STATUS:** stub — diagram body not yet drawn.

## Question this diagram answers

Where do secrets come from, when are they required, and how do they
reach the fetcher subprocess?

## Code of record

- [`internal/secrets/store.go`](../../internal/secrets/store.go) — the
  `Store` interface every backend satisfies.
- [`internal/secrets/env.go`](../../internal/secrets/env.go),
  [`keychain.go`](../../internal/secrets/keychain.go),
  [`memory.go`](../../internal/secrets/memory.go),
  [`merged.go`](../../internal/secrets/merged.go) — the four backends.
- [`internal/secrets/requirements.go`](../../internal/secrets/requirements.go)
  — which keys each fetcher needs.
- [`internal/secrets/environ.go`](../../internal/secrets/environ.go) —
  how secret values are turned into a `cmd.Env` slice for fetcher
  subprocesses.
- [`internal/screens/secrets.go`](../../internal/screens/secrets.go) —
  the editor UI.
- [`internal/root/model.go`](../../internal/root/model.go) — the two
  gating points: pre-Run (`SelectionConfirmedMsg`) and pre-upload
  (`OpenSecretsForReviewMsg`).

## Update when

- A new key is added to `requirements.go` or a key's required/optional
  status changes.
- A new backend is added (or an existing backend's `Writable` /
  `Source` semantics change).
- The "merge" precedence in `merged.go` changes.
- The injection mechanism in `environ.go` (which env-var names get
  set on the subprocess) changes.

## Operator checklist

- Which keys are required vs. optional, and for which fetchers?
- Where does a value live when the user types it in the Secrets screen
  — keychain, memory, or somewhere else?
- What overrides what? (env vs. keychain precedence)
- How does a value get from the `Store` into the fetcher subprocess?
- How does the app behave in headless / CI mode without a keychain?

## Diagram

```eraser
// Eraser DSL goes here.
// Suggested shape:
//   left:   backends (Env, Keychain, Memory) -> Merged Store
//   middle: requirements check (pre-Run gate, pre-upload gate)
//   right:  cmd.Env injection into fetcher subprocess
```
