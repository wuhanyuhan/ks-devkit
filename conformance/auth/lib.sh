#!/usr/bin/env bash
# conformance/auth 测试的共享函数库。
#
# cases/ 下每个脚本开头都 source 它：
#   source "$(dirname "$0")/../lib.sh"
#
# 依赖环境变量（由 run.sh 导出）：
#   TARGET           被测 service 地址，如 http://localhost:8080
#   JWKS             被测 service 信任的 JWKS URL
#   SIGNER_KEY_DIR   签 JWT 的临时私钥目录（由 mock-jwks/generate.sh 生成）
#   VERBOSE          (可选) 非空时输出详细日志

set -euo pipefail

# ----- 编码辅助 -----

# base64url：RFC 7515 无填充的 url-safe base64
base64url() {
    openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# ----- JWT 签发 -----

# sign_jwt: 用 $SIGNER_KEY_DIR/test_private.pem 签一个 RS256 JWT。
#
# 选项：
#   --kid=<string>             JWT header 的 kid（默认 test-key-1）
#   --sub=<string>             sub claim（默认 agent:1）
#   --exp=<unix-ts>            exp claim（默认 now+5min）
#   --aud=<string>             aud claim（可选）
#   --type=<access|mcp_config_ui>  type claim（可选；Spec B 用于区分 ksapp access token 与 mcp_config_ui token）
#   --mcp-server-id=<int>      mcp_server_id claim（可选；type=mcp_config_ui 时必填，校验 McpServerID 是否匹配）
#   --extra=<json>             额外 claims 的 JSON 片段（可选，与 sub/exp/aud/type/mcp_server_id 合并）
sign_jwt() {
    local kid="test-key-1"
    local sub="agent:1"
    local exp="$(($(date +%s) + 300))"
    local aud=""
    local type=""
    local mcp_server_id=""
    local extra="{}"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --kid=*) kid="${1#*=}" ;;
            --sub=*) sub="${1#*=}" ;;
            --exp=*) exp="${1#*=}" ;;
            --aud=*) aud="${1#*=}" ;;
            --type=*) type="${1#*=}" ;;
            --mcp-server-id=*) mcp_server_id="${1#*=}" ;;
            --extra=*) extra="${1#*=}" ;;
            *) echo "sign_jwt: unknown flag $1" >&2; return 1 ;;
        esac
        shift
    done

    local header payload
    header=$(printf '{"alg":"RS256","typ":"JWT","kid":"%s"}' "$kid" | base64url)

    local base
    base=$(jq -cn --arg s "$sub" --argjson e "$exp" '{sub:$s, exp:$e}')
    if [[ -n "$aud" ]]; then
        base=$(echo "$base" | jq -c --arg a "$aud" '. + {aud:$a}')
    fi
    if [[ -n "$type" ]]; then
        base=$(echo "$base" | jq -c --arg t "$type" '. + {type:$t}')
    fi
    if [[ -n "$mcp_server_id" ]]; then
        base=$(echo "$base" | jq -c --argjson m "$mcp_server_id" '. + {mcp_server_id:$m}')
    fi
    payload=$(echo "$base" | jq -c --argjson e "$extra" '. + $e' | base64url)

    local signing_input="${header}.${payload}"
    local sig
    sig=$(echo -n "$signing_input" | \
        openssl dgst -sha256 -sign "$SIGNER_KEY_DIR/test_private.pem" | base64url)

    echo "${signing_input}.${sig}"
}

# ----- HTTP 辅助 -----

# http_status: 发送请求并只返回 HTTP status code（整数）
http_status() {
    curl -s -o /dev/null -w "%{http_code}" "$@"
}

# http_body: 发送请求并返回 body
http_body() {
    curl -s "$@"
}

# http_response: 发送请求并返回 "STATUS BODY"（用换行分）
http_response() {
    local tmpfile status
    tmpfile=$(mktemp)
    status=$(curl -s -o "$tmpfile" -w "%{http_code}" "$@")
    echo "$status"
    cat "$tmpfile"
    rm -f "$tmpfile"
}

# ----- 断言 -----

assert_eq() {
    local actual="$1"
    local expected="$2"
    local msg="${3:-}"
    if [[ "$actual" != "$expected" ]]; then
        echo "ASSERT FAIL: expected '$expected', got '$actual'${msg:+ - $msg}" >&2
        return 1
    fi
    [[ -n "${VERBOSE:-}" ]] && echo "  assert_eq OK: $actual == $expected"
    return 0
}

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local msg="${3:-}"
    if [[ "$haystack" != *"$needle"* ]]; then
        echo "ASSERT FAIL: '$haystack' does not contain '$needle'${msg:+ - $msg}" >&2
        return 1
    fi
    [[ -n "${VERBOSE:-}" ]] && echo "  assert_contains OK"
    return 0
}

assert_json_field() {
    local json="$1"
    local field="$2"
    local expected="$3"
    local actual
    actual=$(echo "$json" | jq -r ".$field // empty")
    assert_eq "$actual" "$expected" "JSON field .$field"
}

# ----- mock-jwks 动态交互 -----

# add_key_to_mock_jwks: 生成新 RSA key pair，追加到 mock-jwks/jwks.json
# （用于 kid_rotation 测试）。追加后 mock-jwks 会在下次请求时返回新 JWKS。
#
# 返回新 key 的私钥 PEM 路径（stdout）。
add_key_to_mock_jwks() {
    local new_kid="$1"
    local mock_dir="$SIGNER_KEY_DIR/.."
    local new_priv="$SIGNER_KEY_DIR/${new_kid}.pem"
    local new_pub="$SIGNER_KEY_DIR/${new_kid}.pub.pem"

    # 生成新 key
    openssl genrsa -out "$new_priv" 2048 2>/dev/null
    openssl rsa -in "$new_priv" -pubout -out "$new_pub" 2>/dev/null

    # 把新 key 加到 jwks.json
    python3 <<PY
import base64, json
from cryptography.hazmat.primitives import serialization

with open("$new_pub", "rb") as f:
    pub = serialization.load_pem_public_key(f.read())

numbers = pub.public_numbers()
def b64url_uint(n):
    b = n.to_bytes((n.bit_length() + 7) // 8, "big")
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()

new_key = {
    "kty": "RSA", "kid": "$new_kid", "alg": "RS256", "use": "sig",
    "n": b64url_uint(numbers.n), "e": b64url_uint(numbers.e),
}

with open("$mock_dir/jwks.json") as f:
    data = json.load(f)
data["keys"].append(new_key)
with open("$mock_dir/jwks.json", "w") as f:
    json.dump(data, f, indent=2)
PY

    echo "$new_priv"
}

# remove_key_from_mock_jwks: 删除 kid 对应的 key（清理用）
remove_key_from_mock_jwks() {
    local kid_to_rm="$1"
    local mock_dir="$SIGNER_KEY_DIR/.."

    python3 <<PY
import json
with open("$mock_dir/jwks.json") as f:
    data = json.load(f)
data["keys"] = [k for k in data["keys"] if k.get("kid") != "$kid_to_rm"]
with open("$mock_dir/jwks.json", "w") as f:
    json.dump(data, f, indent=2)
PY
    rm -f "$SIGNER_KEY_DIR/${kid_to_rm}.pem" "$SIGNER_KEY_DIR/${kid_to_rm}.pub.pem"
}
