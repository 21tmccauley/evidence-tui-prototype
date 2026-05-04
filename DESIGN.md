# paramify-fetcher — Design, Architecture & Integration Plan

This document explains **why** the prototype is shaped the way it is, **what assurances the existing `evidence-fetchers` system provides that we must not lose**, and **exactly what needs to change** to ship a multi-user-ready replacement.

---

## Part 1 — Decisions behind the prototype

### Why Go + Bubble Tea

| Option considered | Why it lost |
|---|---|
| **Rust + ratatui** (the path `the-grabber` is on) | Already exists and is great, but iteration speed on UI work is slow in Rust. The fetchers are bash/python orchestration — the UI surface area is the cost center, not the AWS calls. Picking the language that's fastest to *iterate the UI in* is the right tradeoff. |
| **Python + Textual** | Same language as `evidence-fetchers/` already, beautiful CSS-styled output. Lost on long-term ergonomics: shipping a single static binary, no Python runtime / venv pain on customer machines, much faster startup. |
| **Go + Bubble Tea** ✅ | Single static binary, fast to iterate, mature TUI ecosystem (Lipgloss, Bubbles), clean concurrency primitives that map well to "stream output from N processes." |

### Why mock-first

The fetchers themselves are *known to work*. The risk in this project is the UX. Mocking lets us iterate that view without paying for AWS auth, slow API responses, or a real engagement to test against. The mock also serves as a permanent demo (`--demo` flag), a deterministic regression substrate, and the contract any real runner must satisfy.

### Architecture

```
main.go
  └── internal/root              (Model: routes between screens)
        ├── internal/screens
        │     ├── welcome        (profile / preset picker)
        │     ├── select         (source tree + fetcher table)
        │     ├── run            (streaming, cards, viewport)
        │     └── review         (results, upload)
        ├── internal/components  (header, footer)
        ├── internal/app         (theme, keymap)
        └── internal/mock        (catalog, runner)
```

One model per screen. No screen imports another. Handoff is via typed messages on the bus (`SelectedProfileMsg`, `SelectionConfirmedMsg`, `RunCompleteMsg`, …). Adding a new screen (e.g., "Prerequisites") is a local change.

### The runner is a state machine, not goroutines

Every script "beat" is a `tea.Tick(delay)` returning `BeatMsg{ID, Index, Run}`. A `runIdx` counter on each fetcher invalidates stale ticks from prior attempts after a retry/cancel. Deterministic, testable, no leaked goroutines.

The real runner *will* use goroutines for subprocess pipe reads — but the same `runIdx` invalidation pattern applies, so the run screen's `Update` doesn't change.

### Visual & interaction choices

- Tokyo Night palette — high contrast on dark terminals.
- Persistent header (profile, region, breadcrumb) and footer (per-screen keybinds).
- Run screen: cards on the left (status at a glance), output on the right (detail). Operator never has to switch views.
- Six terminal states: `queued / running / ok / partial / failed / cancelled` plus a `stalled` flag — the most a glanceable card can carry.

---

## Part 2 — User flow audit (existing `evidence-fetchers`)

The Python tool walks the operator through six steps, all driven from `main.py`'s stdout menu:

| Step | What it does | Side effects |
|---|---|---|
| 0. Prerequisites | Checks `.env`, `python3/aws/jq/curl/kubectl` on PATH, pip-installs `requirements.txt` | None beyond pip |
| 1. Select Fetchers | Loads `evidence_fetchers_catalog.json`; interactive y/n per category and script; writes `customer_config.json` + `evidence_sets.json` | Two files at repo root |
| 2. Create Evidence Sets | Reads `evidence_sets.json`, `POST /evidence` per set (idempotent on `referenceId`), optionally uploads the script file as a Paramify artifact | Network calls; one Evidence Set per item |
| 3. Run Fetchers | Subprocess each script with `bash`/`python3`; AWS pre-flight `sts get-caller-identity`; multi-instance fan-out via env var name-spacing; per-fetcher timeout; writes `evidence/<ts>/<name>.json` and `summary.json` | Subprocess execution, JSON files on disk |
| 4. Upload to Paramify | Auto-discovers latest `evidence/<ts>/`, uploads each evidence file as a multipart artifact attached to its Evidence Set | Network calls; `upload_log.json` (CLI mode only) |
| 5/6. Tests / Add fetcher | Smoke tests; interactive scaffold for new fetchers | New script files, catalog updates |

**State persistence is filesystem-based at the repo root.** All cross-step coordination happens via `customer_config.json`, `evidence_sets.json`, `evidence/<ts>/`, and `.env`. There is no daemon, no DB, no global state outside files.

**Key insight for the rewrite:** the catalog (`evidence_fetchers_catalog.json`) is the *contract* between fetchers, the UI, and Paramify. Stable `EVD-<CATEGORY>-<NAME>` IDs are how Paramify knows "this is the same Evidence Set as last time" — they're the idempotency key for the whole pipeline. We do **not** rewrite this schema; we adopt it.

---

## Part 3 — Functionality & assurances we MUST preserve

This is the master list. Anything below missing from v1 is a regression for existing users. References point at `evidence-fetchers/` paths.

### Catalog & schema fidelity

1. **Stable `referenceId` (`EVD-<CATEGORY>-<NAME>`) per fetcher** — Paramify's idempotent get-or-create pivots on this string. Renaming or regenerating these IDs creates duplicate Evidence Sets in customer tenants. Source: `2-create-evidence-sets/paramify_pusher.py` `find_existing_evidence_set`, `get_or_create_evidence_set` (lines 60–135).
2. **Rich-text instructions** — `evidence_sets.json` stores `instructions` as an array of `{type:"p", children:[{bold,code,text}]}` nodes. The Paramify create call flattens them to a string at the wire (`convert_instructions_to_string`), but the rich-text source is what `update_evidence_sets_rich_text.py` round-trips. Don't drop it.
3. **Regex escaping for validation rules** — catalog stores raw regex; output JSON-encodes them so the regex survives JSON-in-JSON storage in Paramify (`generate_evidence_sets.py:47-96`). Each rule is `{id, regex, logic}`. Silent regression breaks customers' validation.
4. **`controls[]` and `solution_capabilities[]`** — present in the catalog, dropped when rendering `evidence_sets.json` in the menu path, but consumed by `extra-supporting-scripts/map_requirements.py`. Preserve them in the catalog at minimum.

### Authentication & correctness

5. **AWS pre-flight `sts get-caller-identity`** — catches the common "I forgot to `aws sso login`" footgun before any fetcher runs (`run_fetchers.py:209-247`). Without it, fetchers silently emit empty evidence with `metadata.account_id="unknown"`.
6. **AWS post-flight evidence validation** — `validate_aws_evidence` (lines 250–277) re-checks the produced evidence file and downgrades exit-0 runs with `account_id=="unknown"` to FAIL. This is the safety net for fetchers that swallow auth errors.
7. **Per-fetcher timeout escalation** — `FETCHER_TIMEOUT` defaults to 300s; per-fetcher override via `<SCRIPT_NAME>_TIMEOUT`; `ssllabs_tls_scan` floors at 3600s (`run_fetchers.py:184-191`). Slow third-party APIs need this.

### Multi-instance correctness

8. **Multi-region / multi-project fan-out via env namespacing** — `AWS_REGION_<N>_*`, `GITLAB_PROJECT_<N>_*`, `CHECKOV_INSTANCE_<N>_*`. `run_fetchers.parse_multi_instance_config` discovers them by regex over `os.environ`. Each instance becomes its own subprocess with provider-specific env injected.
9. **Multi-instance file routing** — `_find_evidence_file_for_instance` *deliberately refuses* prefix-matching for multi-instance to avoid cross-project bleed (line 740). Critical correctness invariant; don't "simplify" it.
10. **Multi-instance share one Evidence Set** — `paramify_pusher.get_evidence_set_info` strips trailing `_(project|region)_\d+` so artifacts from many regions/projects all attach to the same Paramify Evidence Set. Artifact title is `<Set name> - <resource>` so users can tell them apart.

### Operator-facing diagnostics

11. **`error_reason` propagation** — strings like *"AWS authentication missing or invalid; run 'aws sso login --profile X'"* surface in `summary.json` per failed fetcher (`run_fetchers.run_fetcher_*`). Don't reduce to a generic FAIL.
12. **Dependency pre-check (warning, not block)** — `check_tool_dependencies` warns about missing `jq`, `kubectl`, etc. but lets the user proceed. Important: the check is informational; the operator may know their setup better than the script.
13. **Audit trail** — `evidence/<ts>/summary.json` and `upload_log.json` are read by humans debugging customer issues. Both must persist.

### Fetcher contract (so existing fetchers run unmodified)

14. **4 positional args + extra flags** — every fetcher today accepts `<profile> <region> <output_dir> <legacy_csv_path>` (the 4th hardcoded `/dev/null`), then `--profile/--region/--output-dir` plus optional fetcher-specific flags (`run_fetchers.py:495, 627`). The runner must invoke fetchers exactly this way.
15. **Output filename convention** — single-instance: `<output_dir>/<script_name>.json`. Multi-instance: `<output_dir>/<script_name>_<sanitized_resource_id>.json`. Sanitization: `/` → `_`, strip non-alphanumeric (`_sanitize_project_id`).
16. **AWS evidence metadata invariant** — every AWS fetcher's output JSON must have `metadata.account_id` and `metadata.arn`. The post-flight validator depends on these.
17. **Standalone-runnable fetchers** — `fetchers/common/env_loader.sh` and `env_loader.py` make every fetcher runnable bare (`bash fetchers/aws/foo.sh`) for debugging. Preserve.
18. **Per-fetcher extra flags via env** — `<SCRIPT_NAME>_FETCHER` (e.g., `IAM_ROLES_FETCHER=--exclude-aws-managed-roles`) and the legacy `FETCHER_FLAGS_<SCRIPT_NAME>` are how operators tune individual fetchers without editing scripts.

### Upload pipeline

19. **Idempotent Evidence Set create** — `POST /evidence` swallows HTTP 400 "Reference ID already exists" and falls back to `find_existing_evidence_set` (lines 123-129).
20. **Script-artifact dedup** — script artifacts are deduped by `note ~ "Automated evidence collection script:"` filter; evidence-file artifacts are intentionally allowed to duplicate across runs (multiple snapshots over time). Don't change either side without considering both consequences.
21. **Backward-compatible `summary.json` discovery** — `find_summary_file` accepts `summary.json`, `execution_summary.json`, `evidence_summary.json`, or any other JSON validating the schema (lines 502-569). Customer repos in the wild have artifacts in old formats.
22. **Backward-compatible timestamp formats** — three historical `evidence/<ts>/` formats parsed by `find_latest_evidence_directory`. Same reasoning.

### Bugs in the Python flow we should NOT replicate

The audit also surfaced legacy bugs. The rewrite is the cleanup opportunity:

- `select_fetchers.py` writes a *less-featured* `evidence_sets.json` than the standalone `generate_evidence_sets.py` (no rich-text, no regex escape, no `script_file`). Menu users get the worse output. Consolidate to one codepath.
- `select_fetchers.py:175` reads `customer_config['customer_configuration'].get('customer_name')` but the template stores it under `metadata.customer_name`. Summary always shows "Unknown".
- `add_new_fetcher.py` references `../4-tests/run_tests.py` (wrong path; should be `5-tests/`).
- `upload_log.json` is only written by direct CLI invocation of `paramify_pusher.py`, not by the menu path through `upload_to_paramify.py`. Inconsistent audit trail.
- No retries / backoff in upload — long uploads after rate limits fail hard.

---

## Part 4 — Architecture for multi-user deployment

The tool will ship to many customers. Each customer is multiple operators on multiple machines, each with their own AWS profile and Paramify tenant. That changes a few things.

### Principles

1. **Single static binary, no runtime deps.** No Python, no Node, no system Python packages. (We'll continue to require `aws`, `jq`, `kubectl`, `bash`, `python3` *for the fetcher scripts themselves* — but those are operator-side and we pre-flight them.)
2. **Per-user state under XDG paths** — never write inside the install directory or `/etc`.
3. **Embedded catalog, optional override.** The default catalog ships in the binary; `--catalog <path>` overrides for development.
4. **Fetcher scripts ship alongside the binary** in v1. Long-term they should be discovered dynamically (a "fetcher pack" model), but that's after MVP.
5. **No mutable global state in the source** — every interactor takes a config struct or context.
6. **All operations are explicit.** No surprise side effects: writes happen on confirmation, not on key presses.
7. **Failure modes are visible.** Silent skips become loud red banners.

### State layout (per-user)

```
$XDG_CONFIG_HOME/paramify-fetcher/   (~/.config/paramify-fetcher/ on Linux/macOS)
  config.toml                        # global preferences, theme, default region
  presets/                           # named selections (Part 6)
    fedramp-low.toml
    customer-acme-q1.toml
  catalog-override.json              # optional, takes precedence over embedded
  recents.toml                       # last N profiles, last N preset, last run

$XDG_DATA_HOME/paramify-fetcher/     (~/.local/share/paramify-fetcher/)
  evidence/                          # evidence runs (mirrors existing layout)
    2026-05-01T19-22-04Z/
      summary.json
      upload_log.json                # write here unconditionally; close the gap
      <fetcher>/
        stdout.log
        stderr.log
        <script_name>.json
  logs/
    session-2026-05-01T19-22-04Z.log # full TUI event log for support

$XDG_CACHE_HOME/paramify-fetcher/    (~/.cache/paramify-fetcher/)
  catalog-fetched.json               # if/when remote catalog updates ship
  pre-flight-cache.json              # cached `aws sts` results (5 min TTL)
```

Override base via `PARAMIFY_FETCHER_HOME`. Honor XDG envs.

### Layered code architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        TUI (screens)                         │  presentation only
├──────────────────────────────────────────────────────────────┤
│   catalog        runner       uploader      preflight        │  domain
│   presets        secrets      evidence      profiles         │
├──────────────────────────────────────────────────────────────┤
│  exec  ·  filesystem  ·  keychain  ·  http  ·  paramify api  │  adapters
└──────────────────────────────────────────────────────────────┘
```

Each domain package exposes an interface; adapters live behind it. The TUI never imports an adapter directly. This is what makes the runner swap (mock → real), the upload swap (mock → http), and the secrets swap (env → keychain) trivial.

Concretely:

```
internal/
  catalog/          loader.go, schema.go, embedded.go
  runner/           runner.go (interface), mock.go, real.go, messages.go
  presets/          store.go, schema.go
  secrets/          store.go (interface), keychain.go, dotenv.go, memory.go
  preflight/        aws.go, tools.go, paramify.go
  profiles/         aws_config.go
  uploader/         uploader.go (interface), paramify.go, mock.go
  evidence/         dirs.go, summary.go, log.go
```

The `screens/` packages take these interfaces as constructor arguments. The root model wires concrete implementations at startup.

### Distribution

- **macOS / Linux:** GitHub Releases, signed `.tar.gz` per arch, plus a Homebrew tap (`paramify/tap`).
- **Windows:** lower priority; bash fetchers don't run there. Document, gate, defer.
- **Auto-update:** opt-in. Check GitHub Releases on startup, prompt before downloading. Off by default.
- **Telemetry:** none in v1. If added, opt-in only, with a clear list of what's reported.

---

## Part 5 — Secrets / credential management

Existing system: plaintext `.env` at repo root. Fine for solo developer, unacceptable for shipped customer tooling.

### Goals

- Plaintext `.env` keeps working (existing fetchers read it via `env_loader.sh/py`).
- New users get OS keychain by default — no plaintext on disk.
- The TUI is the canonical place to add/edit/test secrets.
- Multiple "profiles" per service supported (multi-region / multi-project / multi-tenant).
- Secrets never appear in logs, screen renders, or stack traces.

### Storage backends (interface)

```go
package secrets

type Store interface {
    Get(key string) (string, bool, error)
    Set(key, value string) error
    Delete(key string) error
    List() ([]string, error)        // names only, never values
    Source() string                  // "keychain", "dotenv", "memory"
}
```

Three implementations:

- **`keychain.Store`** — wraps `github.com/zalando/go-keyring`. Per-user OS credential manager (Keychain Access on macOS, libsecret on Linux, Windows Credential Manager on Windows). Default for new installs.
- **`dotenv.Store`** — reads/writes `.env`. Backwards compatibility for existing repos. Marked as insecure in the UI.
- **`memory.Store`** — for tests; never persists.

A "merged" store reads from keychain first, falls back to dotenv. Writes go to whichever the user picked at setup time.

### Per-service secret schemas

Each service defines what keys it needs and how they're validated:

```toml
# baked into the binary; not user-editable
[[service]]
id = "paramify"
name = "Paramify"
required = ["PARAMIFY_UPLOAD_API_TOKEN"]
optional = ["PARAMIFY_API_BASE_URL"]
validate = "GET /me"   # endpoint to ping for sanity check

[[service]]
id = "aws"
name = "AWS"
managed_externally = true   # uses ~/.aws/config + sts pre-flight, not keychain

[[service]]
id = "knowbe4"
required = ["KNOWBE4_API_KEY"]
optional = ["KNOWBE4_REGION"]   # US|EU|CA|UK|DE
```

The Welcome / Prereq screen shows a status badge per service: **green** (key set + validated), **yellow** (key set, not validated), **red** (missing).

### Secrets screen (new)

A dedicated screen with one row per service:

```
┌ secrets ──────────────────────────────────────────────────────┐
│ ✓ paramify        2 keys set      validated 2 min ago        │
│ ✓ aws             SSO via profile paramify-prod              │
│ ⚠ okta            1 key set       not validated              │
│ ✗ knowbe4         no keys set                                │
│ ⚠ gitlab          1 of 3 projects configured                 │
└──────────────────────────────────────────────────────────────┘
```

Enter on a row opens an inline editor: form for each known key, mask values by default, `t` to test (calls the service's `validate` endpoint), `s` to save (writes to the active store).

### Multi-tenant / multi-instance support

The existing `<SERVICE>_<N>_*` env var pattern is the contract. The Secrets screen exposes "instances" as nested rows under each service:

- GitLab → project-1 (acme/api), project-2 (acme/web), project-3 (…)
- AWS → region-1 (us-east-1, paramify-prod), region-2 (us-west-2, paramify-prod)

Stored as `paramify-fetcher/gitlab/project-1/api_token` etc. in the keychain. At run time, the runner reconstructs `GITLAB_PROJECT_1_API_ACCESS_TOKEN` from these and injects into the subprocess env — keeping the existing fetcher contract.

### Migration from `.env`

On first launch, if `.env` exists at the cwd:

1. Show a one-time prompt: "Detected `.env` at `<path>`. Import to keychain (recommended) or keep using plaintext?"
2. On import: read all known keys, write to keychain, append a comment to `.env` ("# imported to keychain on YYYY-MM-DD; safe to delete this file"). Don't delete — let the operator do it.

---

## Part 6 — Selection persistence and presets

Two distinct concepts that the existing system conflates:

- **Selection** — which fetchers the operator picked for *this* run. Ephemeral until they hit Run.
- **Preset** — a named, reusable selection like "FedRAMP Low" or "Customer ACME Q1". Stored, shareable, may be loaded as the starting point.
- **`evidence_sets.json`** — the *Paramify-bound rendering* of a selection (with rich-text instructions, validation rules, etc.). Generated on demand from a selection + the catalog. Not the source of truth.

### Preset format

```toml
# ~/.config/paramify-fetcher/presets/fedramp-low.toml
name = "FedRAMP Low"
description = "Baseline collection for a FedRAMP Low SSP"
created = 2026-05-01T19:22:04Z
updated = 2026-05-01T19:22:04Z
catalog_version = "0.1.0"
fetchers = [
  "EVD-AWS-CLOUDTRAIL_CONFIGURATION",
  "EVD-AWS-IAM_USERS_GROUPS",
  "EVD-AWS-S3_ENCRYPTION_STATUS",
  # …
]

[overrides]
# per-fetcher flags / timeouts that go into the env at run time
"EVD-AWS-IAM_ROLES" = { flags = "--exclude-aws-managed-roles" }
"EVD-SSLLABS-TLS_SCAN" = { timeout = "3600s" }

[metadata]
customer = "ACME"
framework = "FedRAMP Low"
notes = "First baseline collection"
```

Presets reference fetchers by stable `id`, not by file path or display name — renames don't break presets.

### Lifecycle

- **Welcome screen** lists presets. Enter on one → straight into the run, skipping the Select screen entirely.
- **Select screen** loads the active preset's selection as starting state. `s` to save current selection as a new preset (or update the loaded one). `S` (shift-s) to save-as.
- **Sharing** is just a TOML file. Customers can commit their preset to their repo or share via Slack. The CLI accepts `--preset <path>` to run a preset file directly.
- **Catalog version drift:** when a preset references a fetcher ID that no longer exists in the current catalog, the Select screen surfaces it as "missing" with a one-key remove action. Don't silently drop.

### Re-rendering `evidence_sets.json`

The existing flow's `evidence_sets.json` is recomputable from preset + catalog. The TUI generates it just-in-time before step 2 (Create Evidence Sets in Paramify). We'll keep writing it to disk for compatibility (people have automation that depends on it), but treat the preset as authoritative.

---

## Part 7 — Integration plan (revised)

The phases below are revised given the audit findings. Phase 0 is unchanged from the earlier draft; phases 1–10 are updated.

### Phase 0 — Define the Runner seam (DONE)

Move the mock runner behind a `runner.Runner` interface (`Start(id) → Cmd`, `Cancel(id) → Cmd`, plus the existing message types). Add `--demo` flag that selects mock vs real.

Shipped: `internal/runner/` (interface + public messages + types), `internal/mock/runner_adapter.go` (MockRunner), and `--demo` in `main.go`. The Run screen depends only on the interface; Phase 2's `internal/runner/real.go` drops in without screen changes.

### Phase 1 — Catalog from existing `evidence_fetchers_catalog.json`

**Adopt the existing schema, don't invent a new one.** Reasons:

- The schema is the contract for `EVD-*` IDs, validation rules, controls, solution_capabilities — all of which downstream tooling depends on.
- The catalog is hand-curated; rewriting it would lose history and human review.
- The Python flow can keep working in parallel during the rollout.

`internal/catalog` reads `evidence_fetchers_catalog.json` (embedded by default; overridable via `--catalog`). The Go struct mirrors the JSON exactly:

```go
type Catalog struct {
    EvidenceFetchersCatalog struct {
        Version    string                  `json:"version"`
        Categories map[string]Category     `json:"categories"`
    } `json:"evidence_fetchers_catalog"`
}

type Script struct {
    ScriptFile           string         `json:"script_file"`
    Name                 string         `json:"name"`
    Description          string         `json:"description"`
    ID                   string         `json:"id"`        // EVD-...
    Instructions         string         `json:"instructions"`
    Dependencies         []string       `json:"dependencies"`
    Tags                 []string       `json:"tags"`
    ValidationRules      []any          `json:"validation_rules"`
    SolutionCapabilities []string       `json:"solution_capabilities"`
    Controls             []string       `json:"controls"`
}
```

Loader validates: every `script_file` exists on disk, every `id` is unique, IDs match `EVD-<UPPER>-<UPPER>` shape.

### Phase 2 — Subprocess runner (DONE)

Shipped:
- `internal/runner/exec.go` — `BuildCmd` builds argv per the contract; sets `CWD=<repo>` and `EVIDENCE_DIR=<run-root>/<key>`.
- `internal/runner/timeout.go` — `ResolveTimeout` reads `<KEY>_TIMEOUT` → `FETCHER_TIMEOUT` → 300s default; SSL Labs floor of 3600s.
- `internal/runner/awsflight.go` — `CLIAuthChecker` for `aws sts get-caller-identity` pre-flight; `ValidateAWSEvidence` post-flight (downgrades to `Failed` when `metadata.account_id` or `metadata.arn` is `"unknown"`).
- `internal/runner/real.go` — `RealRunner` with concurrency cap of 4, per-fetcher goroutines, queue, `Cancel` (SIGTERM → SIGKILL via `cmd.Cancel`/`cmd.WaitDelay`), `Retry` (bumps `runIdx` to invalidate stale messages), and once-per-`Start` AWS pre-flight memoization (`awsAuthMu` serializes the check itself, not just the cache write).
- `internal/runner/sender.go` — `Sender` interface and `Bind(Sender)` so screens get async messages from goroutines via `*tea.Program.Send`.
- `internal/screens/run.go` — surfaces `FinishedMsg.ErrorReason` under `Failed` / `Partial` cards.
- `main.go` — `--profile`, `--region`, `--fetcher-repo-root` flags; `buildRealRunner` activates when `--demo=false`. Output root defaults to `<repo>/evidence/<UTC-timestamp>/` for Phase 2 (matches the Python flow); Phase 5 will move it under `$XDG_DATA_HOME` and add a flag for explicit override.

Per-fetcher subdirectory layout (one step ahead of the Python flat layout — Phase 5's `summary.json` writer should expect subdirs):

```
<output-root>/<script-key>/
  stdout.log
  stderr.log
  <script-name>.json   (written by the script itself)
```

Tests: pure-function units for `BuildCmd`, `ResolveTimeout`, `ValidateAWSEvidence`; integration tests with bash fixture scripts (`testdata/repo/fetchers/aws/echo_{ok,fail,unknown_identity}.sh`) cover OK, non-zero-exit, post-flight downgrade, pre-flight failure, and pre-flight memoization across two fetchers in one run.

`internal/runner/real.go` honors the existing fetcher contract:

1. Resolve `<repo>/fetchers/<source>/<file>`.
2. Build argv: `[bash|python3] <script> <profile> <region> <output_dir> /dev/null --profile <profile> --region <region> --output-dir <output_dir> [<extra-flags>]`.
3. CWD = repo root (matches existing behavior).
4. Env = current process env, plus `EVIDENCE_DIR=<output_dir>`, plus injected per-instance overrides for multi-instance.
5. Pipe stdout + stderr; goroutine per pipe forwards lines to `program.Send(OutputMsg{...})`. Backpressure: cap UI lines at 400, but tee everything to `<output_dir>/<id>/{stdout,stderr}.log`.
6. `cmd.Wait()` → exit code → derive Status. Run AWS post-flight validation if source == aws (parse the produced JSON, fail if `metadata.account_id == "unknown"`).
7. Cancel: `SIGTERM` then `SIGKILL` after 5s.

Per-fetcher timeout: read `<SCRIPT_NAME>_TIMEOUT` env var or fall back to `FETCHER_TIMEOUT` or 300s. Special-case `ssllabs_tls_scan` floor of 3600s.

### Phase 3 — Multi-instance support

`internal/runner/multiinstance.go` reimplements `parse_multi_instance_config` and `create_fetcher_instances`:

- Regex-scan env for `^AWS_REGION_(\d+)_(.+)$` and `^GITLAB_PROJECT_(\d+)_(.+)$` and (later) `^CHECKOV_INSTANCE_(\d+)_(.+)$`.
- For each instance × selected fetcher, produce an `Instance{ID: "<base>_project_2", BaseID: "<base>", Env: {...}}`.
- The runner enqueues instances as if they were independent fetchers; the run screen renders one card per instance under the parent.

Output filename: `<output_dir>/<script_name>_<sanitized_id>.json` for instances, sanitized exactly as the Python (`/` → `_`, strip non-alphanumeric).

The summary's evidence-file lookup must use the *exact-match-then-strip-suffix* rule, never prefix matching, mirroring `_find_evidence_file_for_instance`'s comment.

### Phase 4 — Profile / credential pre-flight

- Parse `~/.aws/config` (use `gopkg.in/ini.v1`) to populate the profile picker.
- After selection, run `aws sts get-caller-identity --profile <p>` as pre-flight. Cache result for 5 min in `pre-flight-cache.json`.
- On SSO error, surface "press `o` to run `aws sso login --profile <p>`", shell out, retry.
- Tools pre-check: `aws`, `jq`, `bash`, `python3`, `kubectl`, `curl` on PATH. Warn but allow proceed (parity with existing).

The Prerequisites screen consolidates: AWS check, tool check, Paramify token validation, per-source secret status (Part 5).

### Phase 5 — Output management

Evidence dir layout matches Part 4. `summary.json` schema matches the existing one byte-for-byte (so the existing Python uploader works against it during the migration window).

### Phase 6 — `evidence_sets.json` round-trip

- On entering "Create Evidence Sets" step, generate `evidence_sets.json` from active preset × catalog.
- Use the *richer* path: rich-text instructions + regex-escaped validation rules + `script_file`. Don't replicate `select_fetchers.py`'s reduced output.
- Preserve `controls[]` and `solution_capabilities[]` in the catalog (already kept; just don't strip on render).
- Write to repo root for compatibility *and* mirror to `~/.local/share/paramify-fetcher/evidence/<ts>/evidence_sets.json` for audit.

### Phase 7 — Real Paramify uploader

`internal/uploader/paramify.go` reimplements the relevant `paramify_pusher` calls in Go:

- `GET /evidence?referenceId=<EVD-...>` for idempotent lookup.
- `POST /evidence` with HTTP 400 swallow + lookup fallback.
- `POST /evidence/<id>/artifacts/upload` multipart for evidence files and script artifacts.
- Script-artifact dedup via `note ~ "Automated evidence collection script:"` filter (preserve the literal string).
- Add: exponential backoff on 429 / 5xx (2s, 4s, 8s; max 3 retries).
- Always write `upload_log.json` (close the existing gap where the menu path skips it).

Why reimplement instead of shelling out: secret handling. We already have the API token in our keychain-backed store; passing it cleanly to a Go HTTP client is simpler than exporting it to a Python subprocess env, and avoids requiring Python at runtime.

### Phase 8 — Prerequisites screen

Replaces step 0. Live status per check; auto-rechecks every 30s. Operator can `r` to recheck immediately. Block "Run" if any required check fails; warn but allow if optional.

### Phase 9 — Add-new-fetcher flow

Port `6-add-new-fetcher`'s scaffolder to Go. The TUI's "Add Fetcher" view:

- Picks a category (or creates a new one).
- Generates a starter `.sh` or `.py` from the existing templates (`new_script_template.sh`, `new_script_template.py`).
- Auto-extracts metadata (name, description, dependencies, tags) from the script's header comments — same heuristic as `add_evidence_fetcher.py`.
- Adds an entry to `evidence_fetchers_catalog.json` and `customer_config_template.json`.
- Runs `validate_catalog` equivalent to confirm uniqueness and structure.
- Prints the contribution checklist (commit message, PR URL).

### Phase 10 — Cutover

Once all of the above is green: delete `main.py`, ship the binary, redirect the existing repo's README to `paramify-fetcher`. Keep the existing Python step scripts in the repo for one release as a fallback path.

---

## Part 8 — Testing strategy

The existing tests are smoke-only (file exists, JSON parses, imports work, one mocked Rippling unit test). For a customer-shipped binary that's not enough.

### Pyramid

| Tier | Purpose | Tools |
|---|---|---|
| **Unit** | State machines, parsers, serializers, secret-store backends | stdlib `testing`, `github.com/stretchr/testify` |
| **Component / fixture** | One screen at a time with synthetic message streams | bubbletea's `Update` is pure-ish; drive it directly |
| **Snapshot** | Render-stability for each screen at known states | `github.com/bradleyjkemp/cupaloy` (golden files) |
| **Integration** | Full screen walks, root model + sub-models | the existing `internal/root/smoke_test.go`, expanded |
| **Replay** | Real fetcher stdout recorded once, replayed deterministically through `MockRunner` | record once via real run, store in `testdata/recorded/` |
| **Contract** | Every fetcher's output JSON matches the catalog's expectations | run each fetcher in a sandboxed account, validate output schema |
| **Manual smoke** | Pre-release checklist on a real test account | documented runbook |

### Specific must-test invariants (from Part 3)

- Catalog loader rejects duplicate `id` values.
- Renderer for `evidence_sets.json` produces rich-text and regex-escaped validation rules. Golden file.
- Multi-instance: env vars `AWS_REGION_1_FETCHERS=foo,bar` and `AWS_REGION_2_FETCHERS=baz` produce 3 instances total. Output file routing returns the exact match, not a prefix.
- AWS post-flight: feed it a JSON with `metadata.account_id="unknown"` → status downgrades to fail.
- Idempotent create: 400 with "Reference ID already exists" → fall back to lookup, return same set ID.
- Backoff on 429: triggers retry sequence with expected delays.
- Preset round-trip: load → modify → save → load again → identical.
- Secrets: setting a key never leaves the value in any log, render, or error message. Test by snapshotting all output during a set/get.

### Recorded fetcher fixtures

Once we have a real test account, run each fetcher once with `--record-fixture`, save as `testdata/recorded/<id>.json` plus `testdata/recorded/<id>.stdout`. The mock runner can replay these for screen tests, demos, and CI.

This gives us deterministic CI without needing AWS creds and a way to detect schema drift (the contract test checks recorded fixture against the current catalog's expectations).

### CI

- `go test ./...` on every PR.
- `go vet ./...` and `staticcheck`.
- `go build` for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64. Cross-arch compile only — no need to actually run on each.
- Snapshot test diff comments on PRs.
- Lint `evidence_fetchers_catalog.json` with the loader + validator on every commit.

### Manual pre-release runbook

Before tagging a release:

1. Run against a known test AWS account with the FedRAMP Low preset. Confirm all evidence files land, summary is clean, upload succeeds.
2. Force a pre-flight failure (no `aws sso login`). Confirm UI shows clear error, doesn't proceed.
3. Trigger a hard-fail fetcher (revoke the IAM permission). Confirm card goes red with the actual error.
4. Cancel mid-run. Confirm subprocess gets SIGTERM, card goes cancelled, queue drains.
5. Resize terminal to 80×24 and 200×60. Confirm reflow is sane.
6. Test on macOS arm64 and macOS Intel (most common customer platforms).

---

## Part 9 — Development process

### Branching

Trunk-based: `main` is always shippable. Short-lived feature branches (`feat/runner-seam`, `fix/regex-escape`). Force-push allowed on feature branches before review, never on main.

### Code review

Every change requires one approval. **Two approvals for any change touching:**

- `internal/secrets/` (credential handling)
- `internal/uploader/` (Paramify API)
- `internal/runner/real.go` (subprocess execution; potential RCE surface)
- `evidence_fetchers_catalog.json` (customer-visible schema)

A `CODEOWNERS` file enforces this.

### Commit / PR conventions

- Conventional commits (`feat:`, `fix:`, `chore:`). The release-notes generator depends on the prefix.
- PR body: one-paragraph summary, screenshot or asciinema for UI changes, manual-test steps, mention which Part-3 assurance(s) the change interacts with (or "n/a").

### Versioning

- Semver for the binary.
- The catalog (`evidence_fetchers_catalog.json`) has its own `version` field. Bump independently when the schema changes.
- Release tags (`v0.4.2`) trigger CI to publish artifacts to GitHub Releases and update the Homebrew tap.

### Adding a fetcher (developer-facing)

The new `DEVELOPER_GUIDE.md` (port of the existing one):

1. Copy `templates/new_fetcher.sh` (or `.py`).
2. Run `paramify-fetcher add` (or use the TUI's Add screen). It auto-extracts metadata, generates the `EVD-*` ID, validates uniqueness, updates the catalog.
3. `go test ./...` — the catalog validator runs as part of this.
4. Open a PR. The PR template asks: which controls does this map to, what's the est. duration, what tools are required?

### Issue templates

- **Bug** — version, OS, paste of last 50 lines from `~/.local/share/paramify-fetcher/logs/`, redacted.
- **New fetcher request** — service, control mapping, why existing fetchers don't cover it.
- **Feature** — what, why, who else benefits.

### Contributing fetchers as a community

The existing `evidence-fetchers` repo is public and Apache-licensed. Keep that posture. The Go binary lives in the same repo (or a sibling) and reads the same catalog — outside contributors only need to add bash/python scripts, no Go required to extend coverage.

### Security considerations

- The binary holds Paramify and third-party API tokens at runtime. Never log secret values; never include them in error messages; never write them to crash dumps.
- Scrub `os.Environ()` before printing it anywhere.
- Verify TLS certificates on every outbound call. Don't add a `--insecure` flag.
- Sign release binaries (cosign or similar) so customers can verify provenance.
- Run `gosec` in CI and treat findings as PR blockers unless explicitly waived.

---

## Part 10 — Open questions and effort estimate

### Open questions for the team

1. **Catalog ownership** — does the rewrite consume the existing `evidence_fetchers_catalog.json` as-is, or do we re-curate during the rewrite? Recommendation: consume as-is to avoid scope creep; clean up in a follow-up.
2. **Paramify uploader: shell out or reimplement?** Part 7 picks reimplement (cleaner secret handling, no Python runtime requirement). Confirm with platform team.
3. **Auto-update channel** — opt-in update check, or rely on `brew upgrade`? Recommendation: rely on Homebrew for v1; revisit if customers ask.
4. **Telemetry** — none in v1, but if we add it, what's the minimum useful signal? (Run counts, error categories, no PII or evidence content.)
5. **Windows support** — defer or commit? Bash fetchers don't run there; either we port them to PowerShell (large effort) or document the gap.
6. **The bugs in Part 3's "do not replicate" list** — fix them in the Python flow too, or only in the rewrite? If we fix in Python first, the rewrite has a simpler contract.

### Effort estimate (revised)

| Phase | Description | Est. | Status |
|---|---|---|---|
| 0 | Runner interface + `--demo` flag | 0.5d | DONE |
| 1 | Catalog from existing `evidence_fetchers_catalog.json` (loader + validator) | 1.5d | DONE |
| 2 | Subprocess runner (single-instance, with AWS pre/post-flight, timeouts) | 3d | DONE |
| 3 | Multi-instance fan-out + output routing | 2d | |
| 4 | Profile picker + AWS pre-flight + tools check | 1.5d | |
| 5 | Output dir management + `summary.json` | 1d | |
| 6 | `evidence_sets.json` renderer (rich-text + regex) | 1.5d | |
| 7 | Real Paramify uploader (idempotent, retries, dedup, log) | 2.5d | |
| 8 | Prerequisites screen (consolidated) | 1d | |
| 9 | Add-new-fetcher flow | 1.5d | |
| 10 | Cutover, docs, release engineering | 1.5d | |
| **+** | **Secrets store (keychain + dotenv migration)** | 3d | |
| **+** | **Presets store + Welcome integration** | 1.5d | |
| **+** | **Testing infrastructure (recorded fixtures, snapshots, CI)** | 3d | |
| **+** | **Manual pre-release runbook + first real customer test** | 2d | |

**Total: ~26 engineer-days** to a customer-shippable v1. The first 9 days (Phases 0–4) get a working real-runner demo on one source. Phases 5–7 (~5 days) hit functional parity with the Python flow. The remaining ~12 days are what makes it actually deployable to multiple users.
