# Subprocess management

How the Run screen turns "the user picked these fetchers" into running
processes and back into UI updates.

## Architecture

Who is involved and which way data flows:

```mermaid
flowchart TD
    runScreen[Run screen]
    runner[runner.Real<br/>queue + fillRunning]
    goroutine["fetcher goroutine<br/>execute() + 2 pipe readers"]
    subprocess["bash or python3 subprocess<br/>cmd.Env, cmd.Dir, cmd.Cancel"]
    sender["Sender<br/>tea.Program.Send"]
    logs[stdout.log + stderr.log<br/>per-fetcher on disk]

    runScreen -->|Start, Cancel, Retry| runner
    runner -->|spawn, max 4 concurrent| goroutine
    goroutine -->|exec.CommandContext| subprocess
    subprocess -->|stdout / stderr lines| goroutine
    goroutine --> logs
    goroutine -->|StartedMsg, OutputMsg, FinishedMsg| sender
    sender -->|tea.Msg dispatch| runScreen
```

## Lifecycle of one fetcher

What happens between `Start` and the final message:

```mermaid
sequenceDiagram
    participant Run as Run screen
    participant R as runner.Real
    participant G as fetcher goroutine
    participant P as subprocess

    Run->>R: Start(ids)
    R->>R: queue + fillRunning (cap 4)
    R->>G: go execute(id)
    G->>G: AWS preflight (if AWS fetcher)
    G->>G: open stdout.log + stderr.log
    G->>P: exec.CommandContext(bash or python3, args, env)
    G-->>Run: StartedMsg

    loop until process exits
        P->>G: stdout / stderr line
        G->>G: write line to log file
        G-->>Run: OutputMsg
    end

    P->>G: process exits (or timeout / cancel)
    G->>G: classify: timeout? cancelled? exit code? AWS post-flight?
    G-->>Run: FinishedMsg(status, exitCode, reason)
    R->>R: fillRunning (next from queue)
```

Key facts:

- **Concurrency cap is 4.** Selecting 30 fetchers means 4 run at a
  time; the rest sit in the queue.
- **Each fetcher gets its own goroutine, plus two more for the stdout
  and stderr pipe readers.** They write the line to a per-fetcher log
  file and emit one `OutputMsg` per line.
- **Cancellation is `SIGTERM`, then `SIGKILL` after 5s.** The Run
  screen never calls cancel directly — it sends a `tea.Cmd` that the
  runner translates.
- **Status classification is ordered**: deadline → context-cancelled →
  exit code → AWS post-flight validation. The first match wins.
- **The runner only talks back to the UI through `Sender.Send`.** The
  goroutine never touches the Bubble Tea model directly; that's the
  whole reason the bridge exists.
- **`summary.json` is written when the last fetcher finishes**, not
  per-fetcher.
