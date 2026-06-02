#!/usr/bin/env bash
# 规则：SPEC.md §3 kid not in JWKS → reject（即使拉了 JWKS 也找不到此 kid）
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

# 用不存在于 JWKS 的 kid（但签名仍用真实私钥，为了让 verifier 走到"查表"这一步）
TOKEN=$(sign_jwt --kid=nonexistent-kid --sub=agent:1)

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "未知 kid 应被拒绝"
