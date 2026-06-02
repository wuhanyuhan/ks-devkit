#!/usr/bin/env python3
"""ks-conf-py-decrypt — X25519-ECDH + AES-256-GCM 解密 mock-tool（Python 侧）。

用法:
    echo '<json>' | python3 py-decrypt.py

输入 (stdin, JSON):
    {
      "mcp_privkey_b64":   "base64-std 32B",
      "ephemeral_pubkey":  "base64-std 32B",
      "nonce":             "base64-std 12B",
      "aad_canonical":     "base64-std AAD bytes",
      "ciphertext":        "base64-std ct||tag"
    }

输出 (stdout, JSON):
    { "plaintext_b64": "base64-std 明文" }

退出码:
    - 0:  解密成功
    - 2:  用法错 / JSON 解析错 / 其他异常
    - 20: （保留，本 mock 未实现 AAD 重算对比）
    - 21: privkey / ephemeral_pubkey / nonce 长度错 / base64 解码错
    - 22: GCM tag 校验失败（InvalidTag）
"""
from __future__ import annotations

import base64
import importlib.util
import json
import sys
import types
from pathlib import Path

from cryptography.exceptions import InvalidTag  # type: ignore[import-not-found]

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

x25519_ecdh = _x25519_mod.x25519_ecdh
derive_kek = _x25519_mod.derive_kek
decrypt_aes_gcm = _aesgcm_mod.decrypt_aes_gcm
X25519_PRIVKEY_LEN = _x25519_mod.X25519_PRIVKEY_LEN
X25519_PUBKEY_LEN = _x25519_mod.X25519_PUBKEY_LEN
AES_GCM_NONCE_LEN = _aesgcm_mod.AES_GCM_NONCE_LEN


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
        priv = base64.b64decode(payload["mcp_privkey_b64"], validate=True)
    except Exception as e:
        _exit_len(f"mcp_privkey_b64 解码失败：{e}")
    if len(priv) != X25519_PRIVKEY_LEN:
        _exit_len(f"mcp_privkey 长度 = {len(priv)}, 期望 {X25519_PRIVKEY_LEN}")

    try:
        eph_pub = base64.b64decode(payload["ephemeral_pubkey"], validate=True)
    except Exception as e:
        _exit_len(f"ephemeral_pubkey 解码失败：{e}")
    if len(eph_pub) != X25519_PUBKEY_LEN:
        _exit_len(f"ephemeral_pubkey 长度 = {len(eph_pub)}, 期望 {X25519_PUBKEY_LEN}")

    try:
        nonce = base64.b64decode(payload["nonce"], validate=True)
    except Exception as e:
        _exit_len(f"nonce 解码失败：{e}")
    if len(nonce) != AES_GCM_NONCE_LEN:
        _exit_len(f"nonce 长度 = {len(nonce)}, 期望 {AES_GCM_NONCE_LEN}")

    try:
        aad = base64.b64decode(payload["aad_canonical"], validate=True)
    except Exception as e:
        _exit_len(f"aad_canonical 解码失败：{e}")

    try:
        ct = base64.b64decode(payload["ciphertext"], validate=True)
    except Exception as e:
        _exit_len(f"ciphertext 解码失败：{e}")

    # ECDH + HKDF
    shared = x25519_ecdh(priv, eph_pub)
    kek = derive_kek(shared)

    # AES-256-GCM Open
    try:
        plaintext = decrypt_aes_gcm(kek, nonce, ct, aad)
    except InvalidTag:
        print("GCM tag 校验失败", file=sys.stderr)
        return 22
    except Exception as e:
        print(f"解密失败：{e}", file=sys.stderr)
        return 2

    out = {"plaintext_b64": base64.b64encode(plaintext).decode("ascii")}
    sys.stdout.write(json.dumps(out, separators=(",", ":"), ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
