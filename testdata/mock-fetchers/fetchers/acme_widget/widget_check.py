#!/usr/bin/env python3
"""Mock fetcher representing a customer-authored Acme integration.

Demonstrates that dropping a new folder with a Python script and a
.env.example into fetchers/ surfaces the platform and its expected env
keys with zero Go changes to the TUI.
"""
import json
import os
import sys
import time

evidence_dir = os.environ.get("EVIDENCE_DIR", ".")
os.makedirs(evidence_dir, exist_ok=True)

key = os.environ.get("ACME_API_KEY", "").strip()
region = os.environ.get("ACME_REGION", "").strip() or "us-east-1"
if not key:
    print("ACME_API_KEY is not set — using stub data", file=sys.stderr)

print(f"checking widgets in region={region}")
time.sleep(0.5)

widgets = [
    {"id": "w-001", "status": "healthy"},
    {"id": "w-002", "status": "healthy"},
    {"id": "w-003", "status": "degraded"},
]
for w in widgets:
    print(f"  -> {w['id']} ({w['status']})")

data = {"widgets": widgets, "metadata": {"source": "mock-acme", "region": region}}
out = os.path.join(evidence_dir, "widgets.json")
with open(out, "w") as f:
    json.dump(data, f, indent=2)
print(f"wrote {out}")
sys.exit(0)
