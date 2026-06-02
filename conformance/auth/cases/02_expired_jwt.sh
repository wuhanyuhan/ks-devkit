#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST reject expired
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

PAST=$(($(date +%s) - 60))
TOKEN=$(sign_jwt --kid=test-key-1 --exp=$PAST)

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "过期 JWT 应被拒绝"
