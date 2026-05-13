#!/usr/bin/env python3
"""Mock GitHub repos fetcher."""
import json
import os
import sys
import time

evidence_dir = os.environ.get("EVIDENCE_DIR", ".")
os.makedirs(evidence_dir, exist_ok=True)

org = os.environ.get("GITHUB_ORG", "").strip() or "default-org"
print(f"listing repos for org={org}")

repos = [
    {"name": "evidence-tui-prototype", "visibility": "private"},
    {"name": "evidence-fetchers",      "visibility": "private"},
    {"name": "paramify-docs",          "visibility": "internal"},
]
for r in repos:
    time.sleep(0.2)
    print(f"  -> {r['name']} ({r['visibility']})")

data = {"repos": repos, "metadata": {"source": "mock-github", "org": org}}
out = os.path.join(evidence_dir, "repos.json")
with open(out, "w") as f:
    json.dump(data, f, indent=2)
print(f"wrote {out}")
sys.exit(0)
