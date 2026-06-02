#!/usr/bin/env python3
"""ks-conf-py-encrypt — X25519-ECDH + AES-256-GCM 加密 mock-tool（Python 侧）。

用法:
    echo '<json>' | python3 py-encrypt.py

输入 (stdin, JSON):
    {
      "mcp_pubkey_b64":  "base64-std 32B",
      "mcp_server_id":   "ks-mcp-test",
      "config_version":  123,
      "fingerprint":     "ab12:cd34:...",
      "plaintext_b64":   "base64-std 明文"
    }

输出 (stdout, JSON) — 对齐 EncryptedConfigPayload（idempotency_key 省略）:
    {
      "algorithm":        "x25519-ecdh-aes256gcm-v1",
      "ephemeral_pubkey": "base64-std 32B",
      "nonce":            "base64-std 12B",
      "aad_fields": { "mcp_server_id": ..., "config_version": ..., "fingerprint": ... },
      "aad_canonical":    "base64-std AAD bytes",
      "ciphertext":       "base64-std ct||tag"
    }

退出码:
    - 0:  加密成功
    - 2:  用法错 / JSON 解析错
    - 21: pubkey 长度错 / base64 解码错
"""
from __future__ import annotations

import base64
import importlib.util
import json
import sys
import types
from pathlib import Path

_HERE = Path(__file__).resolve().parent
_SDK_SRC = _HERE.parent.parent.parent / "sdk" / "python" / "src"


def _make_stub_pkg(name: str, path: Path) -> types.ModuleType:
    pkg = types.ModuleType(name)
    pkg.__path__ = [str(path)]
    sys.modules[name] = pkg
    return pkg


def _load_submod(name: str, path: Path) -> types.ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod


_KS_APP_DIR = _SDK_SRC / "ks_app"
_CRYPTO_DIR = _KS_APP_DIR / "crypto"

_make_stub_pkg("ks_app", _KS_APP_DIR)
_make_stub_pkg("ks_app.crypto", _CRYPTO_DIR)
_x25519_mod = _load_submod("ks_app.crypto.x25519", _CRYPTO_DIR / "x25519.py")
_aesgcm_mod = _load_submod("ks_app.crypto.aesgcm", _CRYPTO_DIR / "aesgcm.py")
_aad_mod = _load_submod("ks_app.crypto.aad", _CRYPTO_DIR / "aad.py")

generate_x25519 = _x25519_mod.generate_x25519
x25519_ecdh = _x25519_mod.x25519_ecdh
derive_kek = _x25519_mod.derive_kek
encrypt_aes_gcm = _aesgcm_mod.encrypt_aes_gcm
aad_canonical_bytes = _aad_mod.aad_canonical_bytes
X25519_PUBKEY_LEN = _x25519_mod.X25519_PUBKEY_LEN


def _exit_len(msg: str) -> None:
    print(msg, file=sys.stderr)
    sys.exit(21)


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except Exception as e:
        print(f"JSON 解析失败：{e}", file=sys.stderr)
        return 2

    try:
        mcp_pub = base64.b64decode(payload["mcp_pubkey_b64"], validate=True)
    except Exception as e:
        _exit_len(f"mcp_pubkey_b64 解码失败：{e}")
    if len(mcp_pub) != X25519_PUBKEY_LEN:
        _exit_len(
            f"mcp_pubkey 长度 = {len(mcp_pub)}, 期望 {X25519_PUBKEY_LEN}"
        )

    try:
        plaintext = base64.b64decode(payload["plaintext_b64"], validate=True)
    except Exception as e:
        _exit_len(f"plaintext_b64 解码失败：{e}")

    mcp_server_id = payload["mcp_server_id"]
    config_version = int(payload["config_version"])
    fingerprint = payload["fingerprint"]

    # 生成临时密钥对 + ECDH + HKDF
    eph_priv, eph_pub = generate_x25519()
    shared = x25519_ecdh(eph_priv, mcp_pub)
    kek = derive_kek(shared)

    # AAD canonical
    aad = aad_canonical_bytes(mcp_server_id, config_version, fingerprint)

    # AES-256-GCM
    ct, nonce = encrypt_aes_gcm(kek, plaintext, aad)

    out = {
        "algorithm": "x25519-ecdh-aes256gcm-v1",
        "ephemeral_pubkey": base64.b64encode(eph_pub).decode("ascii"),
        "nonce": base64.b64encode(nonce).decode("ascii"),
        "aad_fields": {
            "mcp_server_id": mcp_server_id,
            "config_version": config_version,
            "fingerprint": fingerprint,
        },
        "aad_canonical": base64.b64encode(aad).decode("ascii"),
        "ciphertext": base64.b64encode(ct).decode("ascii"),
    }
    sys.stdout.write(json.dumps(out, separators=(",", ":"), ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
