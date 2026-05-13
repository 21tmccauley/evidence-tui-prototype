#!/usr/bin/env python3
"""Mock failing fetcher to demonstrate the failure path in the Run screen.

Exits non-zero so the runner reports it as failed; the error tail surfaces
the stderr line the script wrote, mirroring how real fetchers would report
missing credentials.
"""
import os
import sys

token = os.environ.get("GITHUB_TOKEN", "").strip()
if not token:
    print("GITHUB_TOKEN is not set — cannot call api.github.com", file=sys.stderr)
    print("Set GITHUB_TOKEN in your .env and rerun", file=sys.stderr)
    sys.exit(2)

print("would call api.github.com/repos/...")
sys.exit(0)
