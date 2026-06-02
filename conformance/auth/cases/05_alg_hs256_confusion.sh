#!/usr/bin/env bash
# 规则：SPEC.md §3 MUST reject alg=HS256（RS→HS 混淆攻击）
# 攻击者用 JWKS 里的 public key 作为 HMAC secret 签 token，期望 verifier 误把
# public key 当 HMAC key 用。
set -euo pipefail
source "$(dirname "$0")/../lib.sh"

HEADER=$(echo -n '{"alg":"HS256","typ":"JWT","kid":"test-key-1"}' | base64url)
PAYLOAD=$(echo -n '{"sub":"attacker","exp":9999999999}' | base64url)
SIGNING_INPUT="${HEADER}.${PAYLOAD}"

# 用 public key 文件内容作为 HMAC secret
SIG=$(echo -n "$SIGNING_INPUT" | \
    openssl dgst -sha256 -hmac "$(cat "$SIGNER_KEY_DIR/test_public.pem")" -binary | base64url)

TOKEN="${SIGNING_INPUT}.${SIG}"

STATUS=$(http_status -X POST "$TARGET/mcp" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')

assert_eq "$STATUS" "401" "RS→HS 混淆攻击必须被拒绝"
