#!/usr/bin/env bash
# 规则：SPEC.md §3 Valid RS256 JWT w/ known kid → 200
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

TOKEN=$(sign_jwt --kid=test-key-1 --sub=agent:1)

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "200" "合法 JWT 应被接受"
