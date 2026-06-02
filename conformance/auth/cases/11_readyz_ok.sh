#!/usr/bin/env bash
# 规则：SPEC.md §1 /readyz → 200 + {"status":"ok"}
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

RESP=$(http_response "$TARGET/readyz")
STATUS=$(echo "$RESP" | head -n 1)
BODY=$(echo "$RESP" | tail -n +2)

assert_eq "$STATUS" "200" "/readyz 应返回 200"

STATUS_FIELD=$(echo "$BODY" | jq -r '.status // empty')
assert_eq "$STATUS_FIELD" "ok" "/readyz body.status 应为 ok"
