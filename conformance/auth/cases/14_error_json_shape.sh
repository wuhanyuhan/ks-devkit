#!/usr/bin/env bash
# 规则：SPEC.md §5 所有 401 响应 body shape: {"error": string}
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

# 用"无 Authorization"触发 401
BODY=$(http_body "$TARGET/mcp" \
    -X POST -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

# 检查 Content-Type
HEADERS=$(curl -s -D - -o /dev/null "$TARGET/mcp" \
    -X POST -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' 2>&1 || true)

CT=$(echo "$HEADERS" | grep -i "^content-type:" | head -1)
assert_contains "$CT" "application/json" "401 Content-Type 必须是 application/json"

ERR=$(echo "$BODY" | jq -r '.error // empty')
[[ -n "$ERR" ]] || { echo "FAIL: 401 body 必须含 .error 字段"; exit 1; }

echo "  PASS: 401 body shape = {\"error\": string}"
