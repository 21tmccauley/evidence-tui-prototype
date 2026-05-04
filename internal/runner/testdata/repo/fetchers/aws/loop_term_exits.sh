#!/usr/bin/env bash
set -euo pipefail

term() {
  echo "got-term"
  exit 0
}
trap term TERM

echo "started"
while true; do
  sleep 1
done

