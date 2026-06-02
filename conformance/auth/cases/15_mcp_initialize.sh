#!/usr/bin/env bash
# 规则：SPEC.md §6 MCP initialize → protocolVersion 2025-03-26
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

TOKEN=$(sign_jwt --kid=test-key-1)

BODY=$(http_body -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{}}}')

PROTO=$(echo "$BODY" | jq -r '.result.protocolVersion // empty')
assert_eq "$PROTO" "2025-03-26" "initialize 响应 protocolVersion"
