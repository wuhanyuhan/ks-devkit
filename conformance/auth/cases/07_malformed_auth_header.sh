#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST reject malformed Authorization header
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

# 非 Bearer 前缀
STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Basic abcdef" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "非 Bearer 前缀应被拒绝"

# 只有 "Bearer"（无 token）
STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "Bearer 后缺 token 应被拒绝"
