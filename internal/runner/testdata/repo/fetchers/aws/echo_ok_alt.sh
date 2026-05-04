#!/bin/bash
# Same as echo_ok.sh but writes echo_ok_alt.json so two AWS fetchers can run
# concurrently without sharing an output directory (avoids log/json races in tests).
PROFILE="$1"
REGION="$2"
OUTPUT_DIR="$3"

echo "alt-ok-line-1"
echo "alt-ok-line-2"
echo "alt-stderr-line-1" 1>&2

mkdir -p "$OUTPUT_DIR"
cat > "$OUTPUT_DIR/echo_ok_alt.json" <<JSON
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
