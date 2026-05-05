# Architecture Decision Records

An **ADR** is a short, numbered note that captures a non-obvious
architectural decision: the context that forced the choice, the choice
itself, the consequences we accepted, and the alternatives we rejected.

The point of an ADR is to answer the question "*why* did we do this?"
six months from now, when the original conversation is gone and the
code by itself doesn't say.

## When to write one

Write an ADR when:

- You're picking between two real alternatives and the choice will be
  hard to reverse (runner model, secrets backend, embedding vs.
  loading the catalog, sync vs. async upload).
- You're breaking one of the constraints in
  [`../DESIGN-TRUTHS.md`](../DESIGN-TRUTHS.md), or changing one of the
  key seams listed there.
- You're introducing a new top-level package or a new external
  dependency the rest of the codebase will lean on.
- A code reviewer asks "why didn't we just do X?" and the answer is
  longer than a comment.

You do **not** need an ADR for routine refactors, bug fixes, or
implementation details that don't change a contract.

## How to add one

1. Copy [`0000-template.md`](0000-template.md) to
   `NNNN-short-title.md`, where `NNNN` is one greater than the highest
   existing number.
2. Set status to `Proposed` while in PR review; flip to `Accepted` (or
   `Rejected`) when the PR merges.
3. Keep it short. One page is plenty. If a section grows past a few
   paragraphs, the ADR is probably trying to also be a design doc.
4. Never edit an `Accepted` ADR's *decision*. To change it, write a new
   ADR that **supersedes** the old one and update the old one's status
   to `Superseded by NNNN`.

## Possible early ADRs (not yet written)

These are decisions that already exist in the codebase but were never
written down. Promote any of these to a real ADR when you have ten
minutes:

- The `runner.Runner` interface as the seam between screens and
  concurrency.
- Why the catalog is `//go:embed`-ed instead of loaded at runtime.
- Why secrets default to a merged keychain+env store.
- Why Bubble Tea (vs. raw termenv, tcell, or a web UI).

## Index

_(empty — first ADR will go here)_
