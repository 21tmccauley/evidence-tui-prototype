#!/usr/bin/env bash
set -euo pipefail

on_term() {
  echo "term-received"
  # Keep running a bit and emit output that should be dropped if the caller
  # immediately retries (stale runIdx).
  sleep 2
  echo "late-after-term"
  exit 0
}
trap on_term TERM

echo "started"
sleep 0.2
echo "done"

