#!/bin/bash
# Fixture that exits 0 but produces evidence with metadata.account_id="unknown".
# Used to verify the AWS post-flight downgrades exit-0 to FAIL.
OUTPUT_DIR="$3"
mkdir -p "$OUTPUT_DIR"
cat > "$OUTPUT_DIR/echo_unknown_identity.json" <<JSON
{
  "metadata": {
    "account_id": "unknown",
    "arn": "unknown"
  },
  "results": []
}
JSON
exit 0
