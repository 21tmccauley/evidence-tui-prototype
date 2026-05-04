# Fake AWS config/credentials (local testing)

This folder contains **intentionally fake** AWS config/credentials files you can
use to exercise the TUI’s credential-preflight error paths without touching your
real `~/.aws/*`.

## Invalid static keys (STS failure, not “missing credentials”)

Run the TUI with:

```sh
AWS_CONFIG_FILE="$(pwd)/testdata/aws/config" \
AWS_SHARED_CREDENTIALS_FILE="$(pwd)/testdata/aws/credentials" \
go run . --demo=false --fetcher-repo-root ../evidence-fetchers --profile badkeys
```

Expected behavior: the AWS CLI is found, but `aws sts get-caller-identity` fails
with an auth error like `InvalidClientTokenId`.

## Notes

- The welcome-screen profile picker is populated from `~/.aws/config`. To test
  these fake files, pass `--profile badkeys` (as above).
- Do not put real secrets in this directory.

