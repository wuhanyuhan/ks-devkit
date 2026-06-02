#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST reject missing Authorization header
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "缺 Authorization header 应返回 401"
