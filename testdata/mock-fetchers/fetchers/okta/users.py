#!/usr/bin/env python3
"""Mock Okta users fetcher. Reads OKTA_API_TOKEN; falls back to stub data."""
import json
import os
import sys
import time

evidence_dir = os.environ.get("EVIDENCE_DIR", ".")
os.makedirs(evidence_dir, exist_ok=True)

token = os.environ.get("OKTA_API_TOKEN", "").strip()
org = os.environ.get("OKTA_ORG_URL", "").strip()

if not token:
    print("OKTA_API_TOKEN not set — edit your .env to enable real calls", file=sys.stderr)
    print("using stub data", file=sys.stderr)
elif not org:
    print(f"OKTA_API_TOKEN present ({token[:6]}…); OKTA_ORG_URL not set", file=sys.stderr)
    print("would call default org; using stub data", file=sys.stderr)
else:
    print(f"would GET {org}/api/v1/users with token {token[:6]}…")
    print("using stub data (mock fetcher)")

time.sleep(0.4)
print("collected 3 users")

data = {
    "users": [
        {"profile": {"email": "alice@example.com", "login": "alice"}},
        {"profile": {"email": "bob@example.com",   "login": "bob"}},
        {"profile": {"email": "carol@example.com", "login": "carol"}},
    ],
    "metadata": {"source": "mock-okta", "org": org or "unset"},
}
out = os.path.join(evidence_dir, "users.json")
with open(out, "w") as f:
    json.dump(data, f, indent=2)
print(f"wrote {out}")
sys.exit(0)
