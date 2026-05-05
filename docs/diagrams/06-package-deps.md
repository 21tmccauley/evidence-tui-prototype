# 06. Package dependency map

> **STATUS:** optional — promote to "active" only if contributors start
> getting confused about which package may import which. Until then,
> leave this stub in place but do not maintain a body.

## Question this diagram answers

Which packages depend on which, and where are the seams?

## Code of record

- All directories under [`internal/`](../../internal/): `app`,
  `catalog`, `components`, `evidence`, `mock`, `output`, `preflight`,
  `root`, `runner`, `screens`, `secrets`, `uploader`.
- [`main.go`](../../main.go) — the only `package main` and the only
  place wiring happens at the top level.

## Update when

- A new top-level package is added under `internal/`.
- A new import is added that crosses an existing seam (e.g., `screens`
  importing something from `runner` other than the public interface, or
  `secrets` reaching into `uploader`).

## Operator checklist

- What does each package own?
- Which imports are allowed and which would be a smell?
- Where are the seams that let us swap implementations (Runner,
  Store, Uploader)?

## Diagram

```eraser
// Eraser DSL goes here. Boxes per package, arrows for imports.
// Highlight the interface seams:
//   - runner.Runner  (between screens and runner/mock impls)
//   - secrets.Store  (between root/screens and secrets backends)
//   - uploader.Uploader (between review and uploader impls)
```
