#!/bin/bash
# Minimal fixture script. Prints two known stdout lines, one stderr line,
# writes a fake evidence JSON, then exits 0.
PROFILE="$1"
REGION="$2"
OUTPUT_DIR="$3"

echo "ok-line-1"
echo "ok-line-2"
echo "stderr-line-1" 1>&2

mkdir -p "$OUTPUT_DIR"
cat > "$OUTPUT_DIR/echo_ok.json" <<JSON
{
  "metadata": {
    "profile": "$PROFILE",
    "region": "$REGION",
    "account_id": "111122223333",
    "arn": "arn:aws:iam::111122223333:role/test"
  },
  "results": []
}
JSON

exit 0
