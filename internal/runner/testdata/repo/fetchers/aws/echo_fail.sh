#!/bin/bash
# Minimal failure fixture. Emits a known stderr message and exits non-zero.
echo "boom: simulated failure" 1>&2
exit 7
