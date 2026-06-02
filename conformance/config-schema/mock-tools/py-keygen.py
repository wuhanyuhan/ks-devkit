#!/usr/bin/env python3
"""ks-conf-py-keygen — X25519 密钥对 + fingerprint 生成 mock-tool（Python 侧）。

用法:
    python3 py-keygen.py

输入: 无。
输出 (stdout, JSON):
    {
      "privkey_b64":   "base64-std 32B",
      "pubkey_b64":    "base64-std 32B",
      "fingerprint":   "ab12:cd34:..."
    }

实现说明:
    绕过 ks_app 顶层 __init__（会 eager import uvicorn）。x25519.py 依赖
    cryptography 库；fingerprint.py 相对导入 .x25519，所以用 stub-package 套路。
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

if not (_CRYPTO_DIR / "x25519.py").is_file():
    raise SystemExit(f"py-keygen: 找不到 {_CRYPTO_DIR / 'x25519.py'}")

_make_stub_pkg("ks_app", _KS_APP_DIR)
_make_stub_pkg("ks_app.crypto", _CRYPTO_DIR)
_x25519_mod = _load_submod("ks_app.crypto.x25519", _CRYPTO_DIR / "x25519.py")
_fp_mod = _load_submod("ks_app.crypto.fingerprint", _CRYPTO_DIR / "fingerprint.py")
generate_x25519 = _x25519_mod.generate_x25519
fingerprint = _fp_mod.fingerprint


def main() -> int:
    priv, pub = generate_x25519()
    out = {
        "privkey_b64": base64.b64encode(priv).decode("ascii"),
        "pubkey_b64": base64.b64encode(pub).decode("ascii"),
        "fingerprint": fingerprint(pub),
    }
    sys.stdout.write(json.dumps(out, separators=(",", ":"), ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
