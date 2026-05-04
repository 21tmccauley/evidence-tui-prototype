#!/usr/bin/env bash
set -euo pipefail

for i in $(seq 1 600); do
  echo "line-$i"
done

