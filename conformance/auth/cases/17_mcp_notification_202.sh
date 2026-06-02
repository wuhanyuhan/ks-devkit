#!/usr/bin/env bash
# 规则：SPEC.md §6 无 id 的 JSON-RPC 通知 → 202 Accepted 无 body
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

TOKEN=$(sign_jwt --kid=test-key-1)

# 无 "id" 字段 = notification
STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"notifications/initialized"}')

assert_eq "$STATUS" "202" "JSON-RPC notification 应返回 202 Accepted"
