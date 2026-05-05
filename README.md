# Evidence TUI Prototype

A Bubble Tea prototype for selecting evidence fetchers, running them, reviewing
the generated files, and optionally uploading evidence to Paramify.

## Documentation

- Architecture and design: [`docs/README.md`](docs/README.md) (diagrams,
  design truths, ADRs, maintenance rule).
- Code tour for new developers: [`WALKTHROUGH.md`](WALKTHROUGH.md).

## Run The TUI

From this folder:

```sh
go run .
```

By default this runs in demo mode with deterministic mock data. It does not call
AWS or run scripts from the `evidence-fetchers` repo.

## Run Live Fetchers

Live mode runs real scripts from an existing `evidence-fetchers` checkout. Keep
that repo separate; this app only needs a path to it.

```sh
go run . --demo=false --fetcher-repo-root ../evidence-fetchers
```

With a specific AWS profile and region:

```sh
go run . \
  --demo=false \
  --fetcher-repo-root ../evidence-fetchers \
  --profile my-aws-profile \
  --region us-east-1
```

The app checks for local tools used by live runs: `aws`, `jq`, `bash`,
`python3`, `kubectl`, and `curl`.

### Configure Secrets

From the Welcome screen, press `s` to open the Secrets screen and set keys.
By default, the app stores values in OS keychain and injects them into fetcher
subprocess environment at runtime.

For headless/CI workflows, you can still export env vars before launch:

```sh
export KNOWBE4_API_KEY="your-knowbe4-api-key"
export PARAMIFY_UPLOAD_API_TOKEN="..."
go run . --demo=false --fetcher-repo-root ../evidence-fetchers
```

Do not commit real API keys to this repo.

## Flags

`--demo`

Defaults to `true`. Use `--demo=false` to run live fetcher scripts.

`--fetcher-repo-root`

Path to the separate `evidence-fetchers` checkout. Required when
`--demo=false`.

`--profile`

AWS profile to use for live fetcher runs. If omitted, the app can use your
environment/default AWS configuration.

`--region`

AWS region to use for live fetcher runs. If omitted, the app can use
`AWS_DEFAULT_REGION`, `AWS_REGION`, or the region configured for the selected
profile.

`--output-root`

Directory where evidence for this run should be written. If omitted, the app
writes under `PARAMIFY_FETCHER_HOME`, `XDG_DATA_HOME/paramify-fetcher`, or
`~/.local/share/paramify-fetcher`.

Example:

```sh
go run . \
  --demo=false \
  --fetcher-repo-root ../evidence-fetchers \
  --output-root ./tmp/live-run
```

`--catalog`

Development-only override for the embedded
`evidence_fetchers_catalog.json`.

`--secrets-backend`

Secrets backend selection: `merged` (default, keychain first with env fallback),
`keychain`, or `env`.

## Uploading To Paramify

Review upload uses `PARAMIFY_UPLOAD_API_TOKEN` from the Secrets screen or
environment. Optional API override:

```sh
export PARAMIFY_API_BASE_URL="https://app.paramify.com/api/v0"
```

## Useful Local Test

To exercise the AWS credential failure path without touching your real AWS
files:

```sh
AWS_CONFIG_FILE="$(pwd)/testdata/aws/config" \
AWS_SHARED_CREDENTIALS_FILE="$(pwd)/testdata/aws/credentials" \
go run . --demo=false --fetcher-repo-root ../evidence-fetchers --profile badkeys
```
