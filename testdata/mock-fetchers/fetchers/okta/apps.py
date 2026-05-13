#!/usr/bin/env python3
"""Mock Okta apps fetcher."""
import json
import os
import sys
import time

evidence_dir = os.environ.get("EVIDENCE_DIR", ".")
os.makedirs(evidence_dir, exist_ok=True)

print("listing okta applications")
for app in ["github", "slack", "1password", "google-workspace"]:
    time.sleep(0.1)
    print(f"  -> {app}")

data = {
    "apps": ["github", "slack", "1password", "google-workspace"],
    "metadata": {"source": "mock-okta"},
}
out = os.path.join(evidence_dir, "apps.json")
with open(out, "w") as f:
    json.dump(data, f, indent=2)
print(f"wrote {out}")
sys.exit(0)
