# Evidence TUI Prototype

A Bubble Tea prototype for selecting evidence fetchers, running them, reviewing
the generated files, and optionally uploading evidence to Paramify.

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

### KnowBe4 Scans

Before starting the TUI, export your KnowBe4 API key in the same shell:

```sh
export KNOWBE4_API_KEY="your-knowbe4-api-key"
go run . --demo=false --fetcher-repo-root ../evidence-fetchers
```

The key is read from the environment and passed to the KnowBe4 fetcher scripts.
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

## Uploading To Paramify

To use the upload step in the review screen, set:

```sh
export PARAMIFY_UPLOAD_API_TOKEN="..."
```

Optional API override:

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
