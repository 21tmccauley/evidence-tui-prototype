#!/usr/bin/env python3
"""Mock AWS S3 buckets fetcher."""
import json
import os
import sys
import time

evidence_dir = os.environ.get("EVIDENCE_DIR", ".")
os.makedirs(evidence_dir, exist_ok=True)

print("listing s3 buckets")
buckets = ["paramify-logs", "paramify-evidence", "customer-uploads"]
for b in buckets:
    time.sleep(0.15)
    print(f"  -> {b} (versioning=on, encryption=AES256)")

data = {"buckets": buckets, "metadata": {"source": "mock-aws-s3"}}
out = os.path.join(evidence_dir, "s3_buckets.json")
with open(out, "w") as f:
    json.dump(data, f, indent=2)
print(f"wrote {out}")
sys.exit(0)
