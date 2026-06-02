#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST reject alg=none（经典 JWT 攻击）
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

HEADER=$(echo -n '{"alg":"none","typ":"JWT","kid":"test-key-1"}' | base64url)
PAYLOAD=$(echo -n '{"sub":"attacker","exp":9999999999}' | base64url)
TOKEN="${HEADER}.${PAYLOAD}."

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "alg=none 攻击必须被拒绝"
