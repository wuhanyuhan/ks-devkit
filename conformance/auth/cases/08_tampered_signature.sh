#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST reject token with tampered payload
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

TOKEN=$(sign_jwt --kid=test-key-1 --sub=agent:1)

# 篡改 payload（第二段）：把它替换成攻击者想要的 payload，签名保持原样
ATTACKER_PAYLOAD=$(echo -n '{"sub":"attacker","exp":9999999999}' | base64url)
# 分割原 token
IFS='.' read -r HEADER _ SIG <<< "$TOKEN"
TAMPERED="${HEADER}.${ATTACKER_PAYLOAD}.${SIG}"

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TAMPERED" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "篡改 payload 的 token 必须被拒绝"
