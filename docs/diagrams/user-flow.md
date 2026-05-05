# User flow

What an operator does, from launching the tool to being done.

```mermaid
flowchart TD
    start([launch])
    welcome[Welcome<br/>pick AWS profile]
    secrets[Secrets<br/>set / clear values]
    select[Select<br/>pick fetchers]
    run[Run<br/>up to 4 in parallel]
    review[Review<br/>results, export, upload]
    done([done])

    start --> welcome
    welcome -->|press s| secrets
    welcome -->|enter on profile| select
    secrets -->|esc| welcome
    select -->|press s| secrets
    select -->|enter| run
    run -->|all fetchers terminal| review
    review -->|press u, no upload token| secrets
    secrets -->|esc, token now set| review
    review -->|export to disk or upload| done
    review -->|esc| welcome
```

Notes:

- Secrets is reachable from Welcome, Select, and Review. The escape
  destination depends on which screen routed in — the router remembers
  it.
- The Secrets screen lists every catalog source plus a pinned Paramify
  entry. Sources without env-var creds (aws, k8s, ssllabs, …) render an
  info row instead of editable keys.
- **Run is not gated on selection-specific secrets.** The TUI offers a
  place to store keys; deciding which keys a fetcher needs is the
  fetcher's job. Missing keys surface as fetcher failures — the
  operator fixes via Secrets and retries the failed card.
- The upload-token detour from Review *is* a gate: if
  `PARAMIFY_UPLOAD_API_TOKEN` is missing at the moment the user
  presses upload, Review routes through Secrets so the upload can
  proceed.
- `Ctrl+C` / `Q` quit from any screen; not drawn.
