#!/usr/bin/env bash
# 规则：SPEC.md §6 MCP tools/list 返回含 claimant 注册的 echo 工具
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

TOKEN=$(sign_jwt --kid=test-key-1)

BODY=$(http_body -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

# 应该是 {"jsonrpc":"2.0","id":1,"result":{"tools":[...]}}
TOOLS_TYPE=$(echo "$BODY" | jq -r '.result.tools | type')
assert_eq "$TOOLS_TYPE" "array" "result.tools 必须是数组"

HAS_ECHO=$(echo "$BODY" | jq '[.result.tools[] | select(.name=="echo")] | length')
[[ "$HAS_ECHO" -ge 1 ]] || { echo "FAIL: tools/list 未含 echo 工具"; exit 1; }

echo "  PASS: tools/list 含 echo 工具"
