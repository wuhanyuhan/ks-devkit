#!/usr/bin/env bash
# 规则：SPEC.md §3 Spec B 扩展 —— `type=mcp_config_ui` token 必须校验 mcp_server_id 匹配
#       `type=access` token 仍走主干（与 case 01 一致）
# 守护：Spec B §4.5 / Spec A §6.4 双 token 体系
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

# claimant 声明的 McpServerID（约定为 1，三 claimant 一致）
EXPECTED_MCP_SERVER_ID=1
MISMATCH_MCP_SERVER_ID=999

# 场景 A：valid mcp_config_ui token + McpServerID 匹配 → 200
TOKEN_A=$(sign_jwt --kid=test-key-1 --sub=user:1 \
    --type=mcp_config_ui --mcp-server-id="$EXPECTED_MCP_SERVER_ID")

STATUS_A=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN_A" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS_A" "200" "type=mcp_config_ui + McpServerID 匹配应被接受"

# 场景 B：mcp_config_ui token + McpServerID 不匹配 → 403
TOKEN_B=$(sign_jwt --kid=test-key-1 --sub=user:1 \
    --type=mcp_config_ui --mcp-server-id="$MISMATCH_MCP_SERVER_ID")

STATUS_B=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN_B" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS_B" "403" "type=mcp_config_ui + McpServerID 不匹配应返回 403"

# 场景 C：type=access token 走主干 → 200（与 case 01 一致，不校验 mcp_server_id）
TOKEN_C=$(sign_jwt --kid=test-key-1 --sub=agent:1 --type=access)

STATUS_C=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN_C" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS_C" "200" "type=access token 应走主干鉴权流程"

echo "  PASS: mcp_config_ui auth 三场景全通过"
