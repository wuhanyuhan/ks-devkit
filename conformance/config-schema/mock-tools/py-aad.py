#!/usr/bin/env python3
"""ks-conf-py-aad — AAD canonical 字节 mock-tool（Python 侧）。

用法:
    python3 py-aad.py <mcp_server_id> <config_version> <fingerprint>

输出 (stdout):
    hex 小写字符串（无换行/空格），对应 ks_app.crypto.aad.aad_canonical_bytes
    的字节串。用于 conformance 套件字节级互通校验。

实现说明:
    绕过 ks_app 顶层 __init__（会 eager import uvicorn / FastAPI），直接按路径
    加载 aad.py 子模块。aad.py 只 import stdlib struct，没有相对导入。
"""
from __future__ import annotations

import importlib.util
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

if not (_CRYPTO_DIR / "aad.py").is_file():
    raise SystemExit(f"py-aad: 找不到 {_CRYPTO_DIR / 'aad.py'}")

_make_stub_pkg("ks_app", _KS_APP_DIR)
_make_stub_pkg("ks_app.crypto", _CRYPTO_DIR)
_aad_mod = _load_submod("ks_app.crypto.aad", _CRYPTO_DIR / "aad.py")
aad_canonical_bytes = _aad_mod.aad_canonical_bytes


def main() -> int:
    if len(sys.argv) != 4:
        print(
            "usage: py-aad.py <mcp_server_id> <config_version> <fingerprint>",
            file=sys.stderr,
        )
        return 2
    mcp_id = sys.argv[1]
    try:
        version = int(sys.argv[2])
    except ValueError as e:
        print(f"config_version 解析失败：{e}", file=sys.stderr)
        return 2
    fp = sys.argv[3]

    aad = aad_canonical_bytes(mcp_id, version, fp)
    sys.stdout.write(aad.hex())
    return 0


if __name__ == "__main__":
    sys.exit(main())
