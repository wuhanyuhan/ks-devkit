#!/usr/bin/env bash
# 生成 conformance/auth 测试用 RSA key pair 与 JWKS。
# 输出目录只用于本地测试运行，禁止提交生成结果。

set -euo pipefail

OUT_DIR="${1:-}"
if [[ -z "$OUT_DIR" ]]; then
    echo "Usage: $0 <output-dir>" >&2
    exit 2
fi

KEY_DIR="$OUT_DIR/keys"
mkdir -p "$KEY_DIR"

PRIVATE_KEY="$KEY_DIR/test_private.pem"
PUBLIC_KEY="$KEY_DIR/test_public.pem"
JWKS="$OUT_DIR/jwks.json"

openssl genrsa -out "$PRIVATE_KEY" 2048 2>/dev/null
openssl rsa -in "$PRIVATE_KEY" -pubout -out "$PUBLIC_KEY" 2>/dev/null
MODULUS_HEX="$(openssl rsa -in "$PRIVATE_KEY" -noout -modulus 2>/dev/null | cut -d= -f2)"

python3 <<PY
import base64
import json

def b64url_uint(n):
    b = n.to_bytes((n.bit_length() + 7) // 8, "big")
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()

n = int("$MODULUS_HEX", 16)
e = 65537

jwks = {
    "keys": [
        {
            "kty": "RSA",
            "kid": "test-key-1",
            "alg": "RS256",
            "use": "sig",
            "n": b64url_uint(n),
            "e": b64url_uint(e),
        }
    ]
}

with open("$JWKS", "w") as f:
    json.dump(jwks, f, indent=2)
    f.write("\\n")
PY
