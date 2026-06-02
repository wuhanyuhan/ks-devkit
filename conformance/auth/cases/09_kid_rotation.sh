#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST refetch JWKS on unknown kid
# 场景：运行中给 JWKS 加一个新 kid，用新 kid 签 token，verifier 应按需重拉并放行
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

ROTATION_KID="rotation-kid-$(date +%s)"
NEW_PRIV=$(add_key_to_mock_jwks "$ROTATION_KID")

# 清理函数（无论成功失败都清理）
cleanup() {
    remove_key_from_mock_jwks "$ROTATION_KID"
}
trap cleanup EXIT

# 用新 kid 和新私钥签 token（手工，因为 lib.sh 的 sign_jwt 固定用 test_private.pem）
HEADER=$(printf '{"alg":"RS256","typ":"JWT","kid":"%s"}' "$ROTATION_KID" | base64url)
PAYLOAD=$(printf '{"sub":"agent:1","exp":%s}' "$(($(date +%s) + 300))" | base64url)
SIGNING_INPUT="${HEADER}.${PAYLOAD}"
SIG=$(echo -n "$SIGNING_INPUT" | openssl dgst -sha256 -sign "$NEW_PRIV" | base64url)
TOKEN="${SIGNING_INPUT}.${SIG}"

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "200" "新 kid 加入 JWKS 后，verifier 应重拉并放行"
