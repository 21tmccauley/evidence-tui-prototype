# 05. Data flow

> **STATUS:** stub — diagram body not yet drawn.

## Question this diagram answers

What artifacts exist at each stage, from embedded catalog to uploader
payload?

## Code of record

- [`internal/catalog/embedded.go`](../../internal/catalog/embedded.go)
  and [`internal/catalog/embedded/`](../../internal/catalog/embedded/)
  — the JSON catalog baked into the binary at build time.
- [`internal/catalog/loader.go`](../../internal/catalog/loader.go) and
  [`schema.go`](../../internal/catalog/schema.go) — parsing and
  validation.
- [`internal/screens/select.go`](../../internal/screens/select.go) —
  selection -> `[]FetcherID`.
- [`internal/output/paths.go`](../../internal/output/paths.go) and
  [`session.go`](../../internal/output/session.go) — where on disk a
  run writes its log files and evidence directory.
- [`internal/evidence/evidence_sets.go`](../../internal/evidence/evidence_sets.go)
  — how raw fetcher output is bundled into evidence sets.
- [`internal/screens/run.go`](../../internal/screens/run.go) — how
  `OutputMsg`s and finished states accumulate into `RunResult`s.
- [`internal/screens/review.go`](../../internal/screens/review.go) —
  what Review presents and what the upload trigger sends.
- [`internal/uploader/paramify.go`](../../internal/uploader/paramify.go)
  and [`python.go`](../../internal/uploader/python.go) — the upload
  payload(s).

## Update when

- A new artifact type is produced or an existing artifact's layout
  changes.
- The on-disk path scheme changes (output root, per-run directory,
  log file names).
- The uploader contract or payload shape changes.
- A new transformation step is added between fetcher output and upload.

## Operator checklist

- After a run, what files exist on disk and where?
- Which artifacts get uploaded and which stay local?
- What survives between sessions (logs, partial uploads) vs. what is
  re-derived?
- Where would I look to debug "the upload didn't include X"?

## Diagram

```eraser
// Eraser DSL goes here.
// Suggested left-to-right pipeline:
//   embedded JSON catalog
//     -> selection (FetcherIDs)
//     -> run config (profile, region, secrets, output root)
//     -> fetcher subprocess
//     -> log files on disk + OutputMsgs to UI
//     -> RunResults
//     -> evidence sets
//     -> uploader payload
```
