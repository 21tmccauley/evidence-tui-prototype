#!/usr/bin/env python3
"""Mock AWS IAM users fetcher. Pretends to call AWS; uses fixture data."""
import json
import os
import sys
import time

evidence_dir = os.environ.get("EVIDENCE_DIR", ".")
os.makedirs(evidence_dir, exist_ok=True)

print("collecting iam users")
time.sleep(0.3)
print("  -> alice (last-login: 2026-04-30)")
print("  -> bob   (last-login: 2026-05-12)")
print("  -> carol (last-login: never)")

data = {
    "users": [
        {"name": "alice", "last_login": "2026-04-30"},
        {"name": "bob",   "last_login": "2026-05-12"},
        {"name": "carol", "last_login": None},
    ],
    "metadata": {"source": "mock-aws-iam"},
}
out = os.path.join(evidence_dir, "iam_users.json")
with open(out, "w") as f:
    json.dump(data, f, indent=2)
print(f"wrote {out}")
sys.exit(0)
