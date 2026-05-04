# Embedded catalog

`evidence_fetchers_catalog.json` here is a **generated copy** of the canonical
file at:

    evidence-fetchers/1-select-fetchers/evidence_fetchers_catalog.json

`.cursor/rules/00-vision.mdc` keeps `evidence-fetchers/` read-only from this
project. `go:embed` cannot read paths outside the module, so we ship a copy.

## Refreshing

Manual for Phase 1. Phase 10 (release engineering) adds a sync target and a CI
check that fails the build when the two files drift.

```sh
cp ../../../../evidence-fetchers/1-select-fetchers/evidence_fetchers_catalog.json \
   evidence_fetchers_catalog.json
```

Run `go test ./internal/catalog/...` afterwards to confirm the embedded copy
still parses and validates.

## Do not edit by hand

Edits belong in the canonical file under `evidence-fetchers/`. The catalog is
the contract for `EVD-*` IDs; touching it here without touching the source
silently breaks customers.
