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
    select -->|enter, all required keys present| run
    select -->|enter, missing required keys| secrets
    secrets -->|esc, keys now resolved| run
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
- "Required keys" are derived from the current selection: e.g. if a
  KnowBe4 fetcher is selected, `KNOWBE4_API_KEY` is required before
  Run starts.
- The upload-token detour from Review only fires if
  `PARAMIFY_UPLOAD_API_TOKEN` is missing at the moment the user presses
  upload (so a token cleared mid-session is caught).
- `Ctrl+C` / `Q` quit from any screen; not drawn.
